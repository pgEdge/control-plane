package apiv1

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/valkdb/postgresparser"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/postgres/hba"
	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/pgEdge/control-plane/server/internal/validation"
)

// validateAuthFileGUCs rejects postgresql_conf settings that would make
// user-supplied pg_hba_conf/pg_ident_conf entries ineffective. When hba_file
// or ident_file is set, Patroni ignores the pg_hba/pg_ident arrays it manages,
// so the control-plane-generated file (including user entries) would never be
// written. GUC names are case-insensitive in PostgreSQL, so we compare lower.
func validateAuthFileGUCs(conf map[string]any, path validation.Path) []error {
	var errs []error
	for key := range conf {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "hba_file", "ident_file":
			err := fmt.Errorf("%q is not allowed: it overrides the control-plane-managed pg_hba.conf/pg_ident.conf and would make pg_hba_conf/pg_ident_conf entries ineffective", key)
			errs = append(errs, validation.NewError(err, path.AppendMapKey(key)))
		}
	}
	return errs
}

// validatePgHbaConf checks that every non-comment pg_hba_conf entry parses.
// Blank and comment lines are allowed and skipped. Validation is intentionally
// minimal — see server/internal/postgres/hba/parse.go.
func validatePgHbaConf(lines []string, path validation.Path) []error {
	var errs []error
	for i, line := range lines {
		if hba.IsComment(line) {
			continue
		}
		if _, err := hba.ParseEntry(line); err != nil {
			wrapped := fmt.Errorf("invalid pg_hba entry %q: %w", line, err)
			errs = append(errs, validation.NewError(wrapped, path.AppendArrayIndex(i)))
		}
	}
	return errs
}

// validatePgIdentConf checks that every non-comment pg_ident_conf entry parses.
func validatePgIdentConf(lines []string, path validation.Path) []error {
	var errs []error
	for i, line := range lines {
		if hba.IsComment(line) {
			continue
		}
		if _, err := hba.ParseIdent(line); err != nil {
			wrapped := fmt.Errorf("invalid pg_ident entry %q: %w", line, err)
			errs = append(errs, validation.NewError(wrapped, path.AppendArrayIndex(i)))
		}
	}
	return errs
}

func validateDatabaseSpec(orchestrator config.Orchestrator, databaseID string, spec *api.DatabaseSpec) error {
	var errs []error

	errs = append(errs, validateCPUs(spec.Cpus, validation.NewPath("cpus"))...)
	errs = append(errs, validateMemory(spec.Memory, validation.NewPath("memory"))...)
	errs = append(errs, validatePorts(spec.Port, spec.PatroniPort, validation.NewPath("port")))
	errs = append(errs, validateUsers(spec.DatabaseUsers, validation.NewPath("database_users"))...)
	errs = append(errs, validateScripts(spec.Scripts, validation.NewPath("scripts"))...)

	// Track node-name uniqueness and prepare set for cross-node checks.
	seenNodeNames := make(ds.Set[string], len(spec.Nodes))
	// Track nodes that themselves have a source_node (treated as "new" nodes).
	newNodesWithSource := make(ds.Set[string], len(spec.Nodes))

	nodesPath := validation.NewPath("nodes")
	for i, node := range spec.Nodes {
		nodePath := nodesPath.AppendArrayIndex(i)

		if seenNodeNames.Has(node.Name) {
			err := errors.New("node names must be unique within a database")
			errs = append(errs, validation.NewError(err, nodePath))
		}

		seenNodeNames.Add(node.Name)

		// Mark nodes that declare a source_node as "new" nodes.
		if utils.FromPointer(node.SourceNode) != "" {
			newNodesWithSource.Add(node.Name)
		}

		// Per-node validation (includes self-ref and restore vs source_node conflict)
		errs = append(errs, validateNode(orchestrator, spec, node, nodePath)...)
	}

	// Cross-node existence check for source_node
	for i, node := range spec.Nodes {
		src := utils.FromPointer(node.SourceNode)
		if src == "" {
			continue
		}

		srcPath := nodesPath.AppendArrayIndex(i).Append("source_node")

		if !seenNodeNames.Has(src) {
			// Attach error to the specific field path
			errs = append(errs, validation.NewError(errors.New("source node does not exist"),
				srcPath))
			continue
		}

		// prevent using a "new" node (one that has its own source_node)
		// as the source for another node.
		if newNodesWithSource.Has(src) {
			errs = append(errs, validation.NewError(
				errors.New("source node must refer to an existing node"),
				srcPath,
			))
		}
	}

	// Reject postgresql_conf GUCs that would make user-supplied pg_hba/pg_ident
	// entries ineffective, then validate the entries themselves.
	errs = append(errs, validateAuthFileGUCs(spec.PostgresqlConf, validation.NewPath("postgresql_conf"))...)
	errs = append(errs, validatePgHbaConf(spec.PgHbaConf, validation.NewPath("pg_hba_conf"))...)
	errs = append(errs, validatePgIdentConf(spec.PgIdentConf, validation.NewPath("pg_ident_conf"))...)

	if spec.BackupConfig != nil {
		errs = append(errs, validateBackupConfig(spec.BackupConfig, validation.NewPath("backup_config"))...)
	}
	if spec.RestoreConfig != nil {
		errs = append(errs, validateRestoreConfig(spec.RestoreConfig, validation.NewPath("restore_config"))...)
	}

	// Validate orchestrator_opts (spec-level)
	errs = append(errs, validateOrchestratorOpts(spec.OrchestratorOpts, validation.NewPath("orchestrator_opts"))...)

	// Validate services — seed portOwner with Postgres ports so services can't collide with the database.
	portOwner := make(servicePortOwnerMap)
	seedPostgresPorts(spec, portOwner)

	servicesPath := validation.NewPath("services")

	switch orchestrator {
	case config.OrchestratorSystemD:
		if len(spec.Services) != 0 {
			errs = append(errs, validation.NewError(errors.New("services are not yet supported for systemd clusters"), servicesPath))
		}
	default:
		seenServiceIDs := make(ds.Set[string], len(spec.Services))
		for i, svc := range spec.Services {
			svcPath := servicesPath.AppendArrayIndex(i)

			// Check for duplicate service IDs
			if seenServiceIDs.Has(string(svc.ServiceID)) {
				err := errors.New("service IDs must be unique within a database")
				errs = append(errs, validation.NewError(err, svcPath))
			}
			seenServiceIDs.Add(string(svc.ServiceID))

			errs = append(errs, validateServicePortConflicts(svc, svcPath, portOwner)...)
			errs = append(errs, validateServiceSpec(svc, svcPath, false, databaseID, spec.DatabaseUsers, seenNodeNames)...)
		}
	}

	return errors.Join(errs...)
}

func validateDatabaseUpdate(old *database.Spec, new *api.DatabaseSpec) error {
	var errs []error

	// Collect names of nodes that already exist in the old spec.
	existingNodeNames := make(ds.Set[string], len(old.Nodes))
	for _, n := range old.Nodes {
		existingNodeNames.Add(n.Name)
	}

	// For each newly added node, ensure its source_node (if any) refers to an existing node.
	for i, n := range new.Nodes {
		// Only care about newly added nodes (those NOT in existingNodeNames).
		if existingNodeNames.Has(n.Name) {
			continue
		}

		src := utils.FromPointer(n.SourceNode)
		if src == "" {
			continue // no explicit source_node; auto-selector will handle this later
		}

		if !existingNodeNames.Has(src) {
			// Newly added node is trying to use a new/non-existing node as source.
			path := validation.NewPath("nodes", validation.ArrayIndexElement(i), "source_node")
			errs = append(errs, validation.NewError(
				errors.New("source node must refer to an existing node"),
				path,
			))
		}
	}

	// Build the full set of node names from the new spec for cross-validation.
	newNodeNames := make(ds.Set[string], len(new.Nodes))
	for _, n := range new.Nodes {
		newNodeNames.Add(n.Name)
	}

	// Build a set of service IDs that already exist in the deployment. This is used
	// below to distinguish newly added services from existing ones. Currently this
	// distinction only affects MCP services, which have bootstrap-only fields
	// (init_token, init_users) that may only be set during initial provisioning of
	// the service. Because a service can be added to an existing database via
	// update-database, "initial provisioning" means "first time this service_id
	// appears in the spec" — not "the create-database call was used".
	existingServiceIDs := make(ds.Set[string], len(old.Services))
	for _, svc := range old.Services {
		existingServiceIDs.Add(svc.ServiceID)
	}

	// Seed portOwner with Postgres ports so services can't collide with the database.
	portOwner := make(servicePortOwnerMap)
	seedPostgresPorts(new, portOwner)

	// Validate each service. Pass isUpdate=false for services being added for the
	// first time so that bootstrap-only fields are accepted. For service types that
	// have no bootstrap fields (e.g. postgrest) the flag has no effect.
	for i, svc := range new.Services {
		svcPath := validation.NewPath("services", validation.ArrayIndexElement(i))
		isExistingService := existingServiceIDs.Has(string(svc.ServiceID))

		errs = append(errs, validateServicePortConflicts(svc, svcPath, portOwner)...)
		errs = append(errs, validateServiceSpec(svc, svcPath, isExistingService, old.DatabaseID, new.DatabaseUsers, newNodeNames)...)
	}

	return errors.Join(errs...)
}

func validateNode(
	orchestrator config.Orchestrator,
	db *api.DatabaseSpec,
	node *api.DatabaseNodeSpec,
	path validation.Path,
) []error {
	var errs []error

	cpusPath := path.Append("cpus")
	errs = append(errs, validateCPUs(node.Cpus, cpusPath)...)

	memPath := path.Append("memory")
	errs = append(errs, validateMemory(node.Memory, memPath)...)

	port := db.Port
	if node.Port != nil {
		port = node.Port
	}
	patroniPort := db.PatroniPort
	if node.PatroniPort != nil {
		patroniPort = node.PatroniPort
	}
	portPath := path.Append("port")
	errs = append(errs, validatePorts(port, patroniPort, portPath))

	seenHostIDs := make(ds.Set[string], len(node.HostIds))
	for i, h := range node.HostIds {
		hostID := string(h)
		hostPath := path.Append("host_ids").AppendArrayIndex(i)

		errs = append(errs, validateIdentifier(hostID, hostPath))

		if seenHostIDs.Has(hostID) {
			err := errors.New("host IDs must be unique within a node")
			errs = append(errs, validation.NewError(err, hostPath))
		}

		seenHostIDs.Add(hostID)
	}

	// source_node + restore_config validation (field-level)
	src := utils.FromPointer(node.SourceNode)
	srcPath := path.Append("source_node")

	// If restore_config is provided, source_node must be empty
	if node.RestoreConfig != nil && src != "" {
		errs = append(errs, validation.NewError(errors.New("specify either source_node or restore_config"), srcPath))
	} else if src != "" {
		// Self-reference is invalid
		if src == node.Name {
			errs = append(errs, validation.NewError(errors.New("a node cannot use itself as a source node"), srcPath))
		}
	}

	errs = append(errs, validateAuthFileGUCs(node.PostgresqlConf, path.Append("postgresql_conf"))...)
	errs = append(errs, validatePgHbaConf(node.PgHbaConf, path.Append("pg_hba_conf"))...)
	errs = append(errs, validatePgIdentConf(node.PgIdentConf, path.Append("pg_ident_conf"))...)

	if node.BackupConfig != nil {
		backupConfigPath := path.Append("backup_config")
		errs = append(errs, validateBackupConfig(node.BackupConfig, backupConfigPath)...)
	}
	if node.RestoreConfig != nil {
		restoreConfigPath := path.Append("restore_config")
		errs = append(errs, validateRestoreConfig(node.RestoreConfig, restoreConfigPath)...)
	}

	switch orchestrator {
	case config.OrchestratorSystemD:
		if db.Port == nil && node.Port == nil {
			portPath := path.Append("port")
			errs = append(errs, validation.NewError(errors.New("port must be defined"), portPath))
		}
		if db.PatroniPort == nil && node.PatroniPort == nil {
			portPath := path.Append("patroni_port")
			errs = append(errs, validation.NewError(errors.New("patroni_port must be defined"), portPath))
		}
	}

	// Validate orchestrator_opts (per-node)
	errs = append(errs, validateOrchestratorOpts(node.OrchestratorOpts, path.Append("orchestrator_opts"))...)

	return errs
}

func validateServiceSpec(svc *api.ServiceSpec, path validation.Path, isUpdate bool, databaseID string, dbUsers []*api.DatabaseUserSpec, nodeNames ...ds.Set[string]) []error {
	var errs []error

	// Validate service_id
	serviceIDPath := path.Append("service_id")
	errs = append(errs, validateIdentifier(string(svc.ServiceID), serviceIDPath))

	// Enforce Docker Swarm service name budget: "{databaseID}-{serviceID}-{8charHash}" must be ≤63 chars.
	if len(databaseID)+len(string(svc.ServiceID)) > 53 {
		err := fmt.Errorf("database ID and service ID combined must not exceed 53 characters (got %d)", len(databaseID)+len(string(svc.ServiceID)))
		errs = append(errs, validation.NewError(err, serviceIDPath))
	}

	// Validate service_type allowlist
	supportedServiceTypes := validation.NewPath("mcp", "postgrest", "rag")
	if !slices.Contains(supportedServiceTypes, svc.ServiceType) {
		err := fmt.Errorf("unsupported service type %q (supported: %s)",
			svc.ServiceType, strings.Join(supportedServiceTypes, ", "))
		errs = append(errs, validation.NewError(err, path.Append("service_type")))
	}

	// Validate version (semver pattern or "latest")
	if svc.Version != "latest" && !semverPattern.MatchString(svc.Version) {
		err := errors.New("version must be in semver format (e.g., '1.0.0') or 'latest'")
		errs = append(errs, validation.NewError(err, path.Append("version")))
	}

	// Validate host_ids (uniqueness and format)
	seenHostIDs := make(ds.Set[string], len(svc.HostIds))
	for i, hostID := range svc.HostIds {
		hostIDStr := string(hostID)
		hostIDPath := path.Append("host_ids").AppendArrayIndex(i)

		errs = append(errs, validateIdentifier(hostIDStr, hostIDPath))

		// may need to relax this if there is a use-case for multiple service instances on the same host
		if seenHostIDs.Has(hostIDStr) {
			err := errors.New("host IDs must be unique within a service")
			errs = append(errs, validation.NewError(err, hostIDPath))
		}
		seenHostIDs.Add(hostIDStr)
	}

	// Validate config based on service_type
	switch svc.ServiceType {
	case "mcp":
		errs = append(errs, validateMCPServiceConfig(svc.Config, path.Append("config"), isUpdate)...)
	case "postgrest":
		errs = append(errs, validatePostgRESTServiceConfig(svc.Config, path.Append("config"))...)
	case "rag":
		errs = append(errs, validateRAGServiceConfig(svc.Config, path.Append("config"), isUpdate)...)
	}

	// Validate database_connection if provided
	if svc.DatabaseConnection != nil {
		dcPath := path.Append("database_connection")
		var nn ds.Set[string]
		if len(nodeNames) > 0 {
			nn = nodeNames[0]
		}
		errs = append(errs, validateDatabaseConnection(svc.DatabaseConnection, dcPath, nn)...)
	}

	// Validate connect_as references a valid database_users entry.
	errs = append(errs, validateConnectAs(svc, dbUsers, path)...)

	// MCP-specific cross-validation: allow_writes vs target_session_attrs
	if svc.ServiceType == "mcp" && svc.DatabaseConnection != nil && svc.DatabaseConnection.TargetSessionAttrs != nil {
		if allowWrites, ok := svc.Config["allow_writes"].(bool); ok && allowWrites {
			tsa := *svc.DatabaseConnection.TargetSessionAttrs
			writeSafe := map[string]bool{database.TargetSessionAttrsPrimary: true, database.TargetSessionAttrsReadWrite: true}
			if tsa != "" && !writeSafe[tsa] {
				err := fmt.Errorf("allow_writes requires target_session_attrs 'primary' or 'read-write', got '%s'", tsa)
				errs = append(errs, validation.NewError(err, path.Append("database_connection", "target_session_attrs")))
			}
		}
	}

	// Validate cpus if provided
	if svc.Cpus != nil {
		errs = append(errs, validateCPUs(svc.Cpus, path.Append("cpus"))...)
	}

	// Validate memory if provided
	if svc.Memory != nil {
		errs = append(errs, validateMemory(svc.Memory, path.Append("memory"))...)
	}

	// Validate orchestrator_opts (service-specific restrictions on top of shared checks)
	errs = append(errs, validateServiceOrchestratorOpts(svc.OrchestratorOpts, path.Append("orchestrator_opts"))...)

	return errs
}

func validateConnectAs(svc *api.ServiceSpec, dbUsers []*api.DatabaseUserSpec, path validation.Path) []error {
	connectAsPath := path.Append("connect_as")
	if svc.ConnectAs == "" {
		return []error{validation.NewError(errors.New("connect_as is required"), connectAsPath)}
	}

	for _, u := range dbUsers {
		if u.Username == svc.ConnectAs {
			return nil
		}
	}

	err := fmt.Errorf("connect_as %q does not match any database_users entry", svc.ConnectAs)
	return []error{validation.NewError(err, connectAsPath)}
}

func validateMCPServiceConfig(config map[string]any, path validation.Path, isUpdate bool) []error {
	_, errs := database.ParseMCPServiceConfig(config, isUpdate)
	var result []error
	for _, err := range errs {
		result = append(result, validation.NewError(err, path))
	}
	return result
}

func validatePostgRESTServiceConfig(config map[string]any, path validation.Path) []error {
	_, errs := database.ParsePostgRESTServiceConfig(config)
	var result []error
	for _, err := range errs {
		result = append(result, validation.NewError(err, path))
	}
	return result
}

func validateDatabaseConnection(dc *api.DatabaseConnection, path validation.Path, nodeNames ds.Set[string]) []error {
	var errs []error

	// Validate target_nodes: no duplicates, no empty strings, must exist in spec
	if dc.TargetNodes != nil {
		seen := make(ds.Set[string], len(dc.TargetNodes))
		for i, node := range dc.TargetNodes {
			nodePath := path.Append("target_nodes").AppendArrayIndex(i)
			if node == "" {
				errs = append(errs, validation.NewError(errors.New("node name must not be empty"), nodePath))
			} else if nodeNames != nil && !nodeNames.Has(node) {
				errs = append(errs, validation.NewError(fmt.Errorf("node %q does not exist in the database spec", node), nodePath))
			}
			if seen.Has(node) {
				errs = append(errs, validation.NewError(fmt.Errorf("duplicate node name %q", node), nodePath))
			}
			seen.Add(node)
		}
	}

	// Validate target_session_attrs enum (belt-and-suspenders — Goa also validates this)
	if dc.TargetSessionAttrs != nil && *dc.TargetSessionAttrs != "" {
		valid := map[string]bool{
			database.TargetSessionAttrsPrimary:       true,
			database.TargetSessionAttrsPreferStandby: true,
			database.TargetSessionAttrsStandby:       true,
			database.TargetSessionAttrsReadWrite:     true,
			database.TargetSessionAttrsAny:           true,
		}
		if !valid[*dc.TargetSessionAttrs] {
			err := fmt.Errorf("invalid target_session_attrs %q (must be primary, prefer-standby, standby, read-write, or any)", *dc.TargetSessionAttrs)
			errs = append(errs, validation.NewError(err, path.Append("target_session_attrs")))
		}
	}

	return errs
}

func validateRAGServiceConfig(config map[string]any, path validation.Path, isUpdate bool) []error {
	_, errs := database.ParseRAGServiceConfig(config, isUpdate)
	var result []error
	for _, err := range errs {
		result = append(result, validation.NewError(err, path))
	}
	return result
}

func validateCPUs(value *string, path validation.Path) []error {
	var errs []error

	cpus, err := parseCPUs(value)
	if err != nil {
		errs = append(errs, validation.NewError(err, path))
	}
	if cpus != 0 && cpus < 0.001 {
		err := errors.New("cannot be less than 1 millicpu")
		errs = append(errs, validation.NewError(err, path))
	}

	return errs
}

func validateMemory(value *string, path validation.Path) []error {
	var errs []error

	_, err := parseBytes(value)
	if err != nil {
		errs = append(errs, validation.NewError(err, path))
	}

	return errs
}

func validatePorts(postgresPort, patroniPort *int, path validation.Path) error {
	postgres := utils.FromPointer(postgresPort)
	patroni := utils.FromPointer(patroniPort)

	if postgres > 0 && postgres == patroni {
		return validation.NewError(errors.New("postgres and patroni ports must not conflict"), path)
	}

	return nil
}

func validateUsers(users []*api.DatabaseUserSpec, path validation.Path) []error {
	var errs []error

	seenNames := ds.NewSet[string]()
	var hasOwner bool
	for i, user := range users {
		userPath := path.AppendArrayIndex(i)

		if seenNames.Has(user.Username) {
			err := errors.New("usernames must be unique within a database")
			errs = append(errs, validation.NewError(err, userPath))
		}
		if user.DbOwner != nil && *user.DbOwner && hasOwner {
			err := errors.New("cannot have multiple users with db_owner = true")
			errs = append(errs, validation.NewError(err, userPath))
		}

		seenNames.Add(user.Username)

		if user.DbOwner != nil && *user.DbOwner {
			hasOwner = true
		}
	}

	return errs
}

// seedPostgresPorts registers each node's effective Postgres port in the
// portOwner map so that service port validation can detect collisions with
// the database. A node-level port override (node.Port) takes precedence
// over the spec-level default (spec.Port).
func seedPostgresPorts(spec *api.DatabaseSpec, owner servicePortOwnerMap) {
	for _, node := range spec.Nodes {
		pgPort := utils.FromPointer(spec.Port)
		if node.Port != nil {
			pgPort = *node.Port
		}
		if pgPort > 0 {
			for _, hostID := range node.HostIds {
				owner[hostPort{hostID: string(hostID), port: pgPort}] = "postgres"
			}
		}
	}
}

// hostPort identifies a unique (host, port) binding for cross-service
// port conflict detection.
type hostPort struct {
	hostID string
	port   int
}

// servicePortOwnerMap tracks which service owns a given (host, port) pair.
// Callers create one map and pass it to validateServicePortConflicts for
// each service in the spec.
type servicePortOwnerMap map[hostPort]string

// validateServicePortConflicts checks that the service's explicit port (if any)
// does not collide with a port already claimed by another service on the same host.
func validateServicePortConflicts(svc *api.ServiceSpec, path validation.Path, owner servicePortOwnerMap) []error {
	if svc.Port == nil || *svc.Port <= 0 {
		return nil
	}

	var errs []error
	for _, hostID := range svc.HostIds {
		key := hostPort{hostID: string(hostID), port: *svc.Port}
		if prev, exists := owner[key]; exists {
			err := fmt.Errorf("port %d conflicts with service %q on the same host", *svc.Port, prev)
			errs = append(errs, validation.NewError(err, path.Append("port")))
		} else {
			owner[key] = string(svc.ServiceID)
		}
	}
	return errs
}

func validateBackupConfig(cfg *api.BackupConfigSpec, path validation.Path) []error {
	var errs []error

	for i, repo := range cfg.Repositories {
		repoPath := path.Append("repositories").AppendArrayIndex(i)
		errs = append(errs, validateBackupRepository(repo, repoPath)...)
	}

	return errs
}

func validateRestoreConfig(cfg *api.RestoreConfigSpec, path validation.Path) []error {
	var errs []error

	sourceDbIdPath := path.Append("source_database_id")
	errs = append(errs, validateIdentifier(string(cfg.SourceDatabaseID), sourceDbIdPath))

	repoPath := path.Append("repository")
	errs = append(errs, validateRestoreRepository(cfg.Repository, repoPath)...)

	restoreOptsPath := path.Append("restore_options")
	errs = append(errs, validatePgBackRestOptions(cfg.RestoreOptions, restoreOptsPath)...)

	return errs
}

func validateBackupRepository(cfg *api.BackupRepositorySpec, path validation.Path) []error {
	props := repoProperties{
		id:             cfg.ID,
		repoType:       cfg.Type,
		azureAccount:   cfg.AzureAccount,
		azureContainer: cfg.AzureContainer,
		azureKey:       cfg.AzureKey,
		basePath:       cfg.BasePath,
		gcsBucket:      cfg.GcsBucket,
		s3Bucket:       cfg.S3Bucket,
		s3Region:       cfg.S3Region,
		customOptions:  cfg.CustomOptions,
	}

	return validateRepoProperties(props, path)
}

func validateRestoreRepository(cfg *api.RestoreRepositorySpec, path validation.Path) []error {
	props := repoProperties{
		id:             cfg.ID,
		repoType:       cfg.Type,
		azureAccount:   cfg.AzureAccount,
		azureContainer: cfg.AzureContainer,
		azureKey:       cfg.AzureKey,
		basePath:       cfg.BasePath,
		gcsBucket:      cfg.GcsBucket,
		s3Bucket:       cfg.S3Bucket,
		s3Region:       cfg.S3Region,
		customOptions:  cfg.CustomOptions,
	}

	return validateRepoProperties(props, path)
}

type repoProperties struct {
	id             *api.Identifier
	repoType       string
	azureAccount   *string
	azureContainer *string
	azureKey       *string
	basePath       *string
	gcsBucket      *string
	s3Bucket       *string
	s3Region       *string
	customOptions  map[string]string
}

func validateRepoProperties(props repoProperties, path validation.Path) []error {
	var errs []error

	id := utils.FromPointer(props.id)
	if id != "" {
		idPath := path.Append("id")
		errs = append(errs, validateIdentifier(string(id), idPath))
	}

	repoType := pgbackrest.RepositoryType(props.repoType)
	switch repoType {
	case pgbackrest.RepositoryTypeAzure:
		errs = append(errs, validateAzureRepoProperties(props, path)...)
	case pgbackrest.RepositoryTypeCifs, pgbackrest.RepositoryTypePosix:
		errs = append(errs, validateFSRepoProperties(props, path)...)
	case pgbackrest.RepositoryTypeGCS:
		errs = append(errs, validateGCSRepoProperties(props, path)...)
	case pgbackrest.RepositoryTypeS3:
		errs = append(errs, validateS3RepoProperties(props, path)...)
	default:
		err := validation.NewError(
			fmt.Errorf("unsupported repo type '%s'", repoType),
			path.Append("type"),
		)
		errs = append(errs, err)
	}

	customOptsPath := path.Append("custom_options")
	errs = append(errs, validatePgBackRestOptions(props.customOptions, customOptsPath)...)

	return errs
}

func validateAzureRepoProperties(props repoProperties, path validation.Path) []error {
	var errs []error

	if utils.FromPointer(props.azureAccount) == "" {
		err := errors.New("azure_account is required for azure repositories")
		errs = append(errs, validation.NewError(err, path.Append("azure_account")))
	}
	if utils.FromPointer(props.azureContainer) == "" {
		err := errors.New("azure_container is required for azure repositories")
		errs = append(errs, validation.NewError(err, path.Append("azure_container")))
	}
	if utils.FromPointer(props.azureKey) == "" {
		err := errors.New("azure_key is required for azure repositories")
		errs = append(errs, validation.NewError(err, path.Append("azure_key")))
	}

	return errs
}

func validateFSRepoProperties(props repoProperties, path validation.Path) []error {
	var errs []error

	basePath := utils.FromPointer(props.basePath)
	if basePath == "" {
		err := fmt.Errorf("base_path is required for %s repositories", props.repoType)
		errs = append(errs, validation.NewError(err, path.Append("base_path")))
	} else if !filepath.IsAbs(*props.basePath) {
		err := fmt.Errorf("base_path must be absolute for %s repositories", props.repoType)
		errs = append(errs, validation.NewError(err, path.Append("base_path")))
	}

	return errs
}

func validateGCSRepoProperties(props repoProperties, path validation.Path) []error {
	var errs []error

	if utils.FromPointer(props.gcsBucket) == "" {
		err := errors.New("gcs_bucket is required for gcs repositories")
		errs = append(errs, validation.NewError(err, path.Append("gcs_bucket")))
	}

	return errs
}

func validateS3RepoProperties(props repoProperties, path validation.Path) []error {
	var errs []error

	if utils.FromPointer(props.s3Bucket) == "" {
		err := errors.New("s3_bucket is required for s3 repositories")
		errs = append(errs, validation.NewError(err, path.Append("s3_bucket")))
	}

	return errs
}

var pgBackRestOptionPattern = regexp.MustCompile(`^[a-z0-9-]+$`)
var semverPattern = regexp.MustCompile(`^\d+\.\d+(\.\d+)?$`)

// reservedLabelPrefix is the label key prefix reserved for system use.
const reservedLabelPrefix = "pgedge."

func validateOrchestratorOpts(opts *api.OrchestratorOpts, path validation.Path) []error {
	if opts == nil || opts.Swarm == nil {
		return nil
	}

	var errs []error
	for key := range opts.Swarm.ExtraLabels {
		if strings.HasPrefix(key, reservedLabelPrefix) {
			labelPath := path.Append("swarm", "extra_labels").AppendMapKey(key)
			err := fmt.Errorf("labels starting with %q are reserved for system use", reservedLabelPrefix)
			errs = append(errs, validation.NewError(err, labelPath))
		}
	}
	return errs
}

// validateServiceOrchestratorOpts runs the shared orchestrator_opts checks and
// adds service-specific restrictions. Services do not support extra_volumes
// (bind mounts are configured per service type) or driver_opts on extra_networks.
func validateServiceOrchestratorOpts(opts *api.OrchestratorOpts, path validation.Path) []error {
	errs := validateOrchestratorOpts(opts, path)

	if opts == nil || opts.Swarm == nil {
		return errs
	}

	if len(opts.Swarm.ExtraVolumes) > 0 {
		err := errors.New("extra_volumes is not supported for services")
		errs = append(errs, validation.NewError(err, path.Append("swarm", "extra_volumes")))
	}

	for i, net := range opts.Swarm.ExtraNetworks {
		if len(net.DriverOpts) > 0 {
			netPath := path.Append("swarm", "extra_networks").AppendArrayIndex(i).Append("driver_opts")
			err := errors.New("driver_opts is not supported for services")
			errs = append(errs, validation.NewError(err, netPath))
		}
	}

	return errs
}

func validatePgBackRestOptions(opts map[string]string, path validation.Path) []error {
	var errs []error

	for key := range opts {
		if !pgBackRestOptionPattern.MatchString(key) {
			optPath := path.AppendMapKey(key)
			err := errors.New("invalid option name")
			errs = append(errs, validation.NewError(err, optPath))
		}
	}

	return errs
}

func validateBackupOptions(opts *api.BackupOptions) error {
	var errs []error

	optsPath := validation.NewPath("backup_options")
	errs = append(errs, validatePgBackRestOptions(opts.BackupOptions, optsPath)...)

	return errors.Join(errs...)
}

func validateIdentifier(ident string, path validation.Path) error {
	if err := utils.ValidateID(ident); err != nil {
		return validation.NewError(err, path)
	}

	return nil
}

// validateHostIDUniqueness checks that the given host ID does not already exist in the cluster.
// Returns an error if the host ID already exists.
func validateHostIDUniqueness(ctx context.Context, hostSvc *host.Service, hostID string) error {
	_, err := hostSvc.GetHost(ctx, hostID)
	switch {
	case err == nil:
		// Host already exists - this is a duplicate
		return ErrHostAlreadyExistsWithID(hostID)
	case errors.Is(err, storage.ErrNotFound):
		// Host doesn't exist - good, host ID is unique
		return nil
	default:
		// Other errors (connection failures, permission errors, etc.) should be propagated
		return fmt.Errorf("failed to check for existing host: %w", err)
	}
}

func validateScripts(scripts *api.DatabaseScripts, path validation.Path) []error {
	if scripts == nil {
		return nil
	}
	return slices.Concat(
		validateScript(scripts.PostInit, path.Append("post_init")),
		validateScript(scripts.PostDatabaseCreate, path.Append("post_database_create")),
	)
}

func validateScript(statements []string, path validation.Path) []error {
	var errs []error
	for i, statement := range statements {
		statementPath := path.AppendArrayIndex(i)
		if err := validateSQLStatement(statement, statementPath); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func validateSQLStatement(statement string, path validation.Path) error {
	_, err := postgresparser.ParseSQLStrict(statement)
	if err != nil {
		err = fmt.Errorf("failed to parse SQL statement: %w", err)
		return validation.NewError(err, path)
	}
	return nil
}
