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
	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type validationError struct {
	path []string
	err  error
}

func newValidationError(err error, path []string) *validationError {
	return &validationError{
		path: path,
		err:  err,
	}
}

func (v *validationError) Unwrap() error {
	return v.err
}

func (v *validationError) Error() string {
	if len(v.path) == 0 {
		return v.err.Error()
	}

	var path strings.Builder
	for i, ele := range v.path {
		if i > 0 && !strings.HasPrefix(ele, "[") {
			path.WriteString(".")
		}
		path.WriteString(ele)
	}
	return fmt.Sprintf("%s: %s", path.String(), v.err.Error())
}

func arrayIndexPath(idx int) string {
	return fmt.Sprintf("[%d]", idx)
}

func mapKeyPath(key string) string {
	return fmt.Sprintf("[%s]", key)
}

func appendPath(path []string, new ...string) []string {
	return append(slices.Clone(path), new...)
}

func validateDatabaseSpec(orchestrator config.Orchestrator, spec *api.DatabaseSpec) error {
	var errs []error

	errs = append(errs, validateCPUs(spec.Cpus, []string{"cpus"})...)
	errs = append(errs, validateMemory(spec.Memory, []string{"memory"})...)
	errs = append(errs, validatePorts(spec.Port, spec.PatroniPort, []string{"port"}))
	errs = append(errs, validateUsers(spec.DatabaseUsers, []string{"database_users"})...)
	errs = append(errs, validateScripts(spec.Scripts, []string{"scripts"})...)

	// Track node-name uniqueness and prepare set for cross-node checks.
	seenNodeNames := make(ds.Set[string], len(spec.Nodes))
	// Track nodes that themselves have a source_node (treated as "new" nodes).
	newNodesWithSource := make(ds.Set[string], len(spec.Nodes))

	for i, node := range spec.Nodes {
		nodePath := []string{"nodes", arrayIndexPath(i)}

		if seenNodeNames.Has(node.Name) {
			err := errors.New("node names must be unique within a database")
			errs = append(errs, newValidationError(err, nodePath))
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

		srcPath := []string{"nodes", arrayIndexPath(i), "source_node"}

		if !seenNodeNames.Has(src) {
			// Attach error to the specific field path
			errs = append(errs, newValidationError(errors.New("source node does not exist"),
				srcPath))
			continue
		}

		// prevent using a "new" node (one that has its own source_node)
		// as the source for another node.
		if newNodesWithSource.Has(src) {
			errs = append(errs, newValidationError(
				errors.New("source node must refer to an existing node"),
				srcPath,
			))
		}
	}

	if spec.BackupConfig != nil {
		errs = append(errs, validateBackupConfig(spec.BackupConfig, []string{"backup_config"})...)
	}
	if spec.RestoreConfig != nil {
		errs = append(errs, validateRestoreConfig(spec.RestoreConfig, []string{"restore_config"})...)
	}

	// Validate orchestrator_opts (spec-level)
	errs = append(errs, validateOrchestratorOpts(spec.OrchestratorOpts, []string{"orchestrator_opts"})...)

	// Validate services — seed portOwner with Postgres ports so services can't collide with the database.
	portOwner := make(servicePortOwnerMap)
	seedPostgresPorts(spec, portOwner)

	seenServiceIDs := make(ds.Set[string], len(spec.Services))
	for i, svc := range spec.Services {
		svcPath := []string{"services", arrayIndexPath(i)}

		// Check for duplicate service IDs
		if seenServiceIDs.Has(string(svc.ServiceID)) {
			err := errors.New("service IDs must be unique within a database")
			errs = append(errs, newValidationError(err, svcPath))
		}
		seenServiceIDs.Add(string(svc.ServiceID))

		errs = append(errs, validateServicePortConflicts(svc, svcPath, portOwner)...)
		errs = append(errs, validateServiceSpec(svc, svcPath, false, spec.DatabaseUsers, seenNodeNames)...)
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
			path := []string{"nodes", arrayIndexPath(i), "source_node"}
			errs = append(errs, newValidationError(
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
		svcPath := []string{"services", arrayIndexPath(i)}
		isExistingService := existingServiceIDs.Has(string(svc.ServiceID))

		errs = append(errs, validateServicePortConflicts(svc, svcPath, portOwner)...)
		errs = append(errs, validateServiceSpec(svc, svcPath, isExistingService, new.DatabaseUsers, newNodeNames)...)
	}

	return errors.Join(errs...)
}

func validateNode(
	orchestrator config.Orchestrator,
	db *api.DatabaseSpec,
	node *api.DatabaseNodeSpec,
	path []string,
) []error {
	var errs []error

	cpusPath := appendPath(path, "cpus")
	errs = append(errs, validateCPUs(node.Cpus, cpusPath)...)

	memPath := appendPath(path, "memory")
	errs = append(errs, validateMemory(node.Memory, memPath)...)

	port := db.Port
	if node.Port != nil {
		port = node.Port
	}
	patroniPort := db.PatroniPort
	if node.PatroniPort != nil {
		patroniPort = node.PatroniPort
	}
	portPath := appendPath(path, "port")
	errs = append(errs, validatePorts(port, patroniPort, portPath))

	seenHostIDs := make(ds.Set[string], len(node.HostIds))
	for i, h := range node.HostIds {
		hostID := string(h)
		hostPath := appendPath(path, "host_ids", arrayIndexPath(i))

		errs = append(errs, validateIdentifier(hostID, hostPath))

		if seenHostIDs.Has(hostID) {
			err := errors.New("host IDs must be unique within a node")
			errs = append(errs, newValidationError(err, hostPath))
		}

		seenHostIDs.Add(hostID)
	}

	// source_node + restore_config validation (field-level)
	src := utils.FromPointer(node.SourceNode)
	srcPath := appendPath(path, "source_node")

	// If restore_config is provided, source_node must be empty
	if node.RestoreConfig != nil && src != "" {
		errs = append(errs, newValidationError(errors.New("specify either source_node or restore_config"), srcPath))
	} else if src != "" {
		// Self-reference is invalid
		if src == node.Name {
			errs = append(errs, newValidationError(errors.New("a node cannot use itself as a source node"), srcPath))
		}
	}

	if node.BackupConfig != nil {
		backupConfigPath := appendPath(path, "backup_config")
		errs = append(errs, validateBackupConfig(node.BackupConfig, backupConfigPath)...)
	}
	if node.RestoreConfig != nil {
		restoreConfigPath := appendPath(path, "restore_config")
		errs = append(errs, validateRestoreConfig(node.RestoreConfig, restoreConfigPath)...)
	}

	switch orchestrator {
	case config.OrchestratorSystemD:
		if db.Port == nil && node.Port == nil {
			portPath := appendPath(path, "port")
			errs = append(errs, newValidationError(errors.New("port must be defined"), portPath))
		}
		if db.PatroniPort == nil && node.PatroniPort == nil {
			portPath := appendPath(path, "patroni_port")
			errs = append(errs, newValidationError(errors.New("patroni_port must be defined"), portPath))
		}
	}

	// Validate orchestrator_opts (per-node)
	errs = append(errs, validateOrchestratorOpts(node.OrchestratorOpts, appendPath(path, "orchestrator_opts"))...)

	return errs
}

func validateServiceSpec(svc *api.ServiceSpec, path []string, isUpdate bool, dbUsers []*api.DatabaseUserSpec, nodeNames ...ds.Set[string]) []error {
	var errs []error

	// Validate service_id
	serviceIDPath := appendPath(path, "service_id")
	errs = append(errs, validateIdentifier(string(svc.ServiceID), serviceIDPath))

	// Validate service_type allowlist
	supportedServiceTypes := []string{"mcp", "postgrest", "rag"}
	if !slices.Contains(supportedServiceTypes, svc.ServiceType) {
		err := fmt.Errorf("unsupported service type %q (supported: %s)",
			svc.ServiceType, strings.Join(supportedServiceTypes, ", "))
		errs = append(errs, newValidationError(err, appendPath(path, "service_type")))
	}

	// Validate version (semver pattern or "latest")
	if svc.Version != "latest" && !semverPattern.MatchString(svc.Version) {
		err := errors.New("version must be in semver format (e.g., '1.0.0') or 'latest'")
		errs = append(errs, newValidationError(err, appendPath(path, "version")))
	}

	// Validate host_ids (uniqueness and format)
	seenHostIDs := make(ds.Set[string], len(svc.HostIds))
	for i, hostID := range svc.HostIds {
		hostIDStr := string(hostID)
		hostIDPath := appendPath(path, "host_ids", arrayIndexPath(i))

		errs = append(errs, validateIdentifier(hostIDStr, hostIDPath))

		// may need to relax this if there is a use-case for multiple service instances on the same host
		if seenHostIDs.Has(hostIDStr) {
			err := errors.New("host IDs must be unique within a service")
			errs = append(errs, newValidationError(err, hostIDPath))
		}
		seenHostIDs.Add(hostIDStr)
	}

	// Validate config based on service_type
	switch svc.ServiceType {
	case "mcp":
		errs = append(errs, validateMCPServiceConfig(svc.Config, appendPath(path, "config"), isUpdate)...)
	case "postgrest":
		errs = append(errs, validatePostgRESTServiceConfig(svc.Config, appendPath(path, "config"))...)
	case "rag":
		errs = append(errs, validateRAGServiceConfig(svc.Config, appendPath(path, "config"), isUpdate)...)
	}

	// Validate database_connection if provided
	if svc.DatabaseConnection != nil {
		dcPath := appendPath(path, "database_connection")
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
				errs = append(errs, newValidationError(err, appendPath(path, "database_connection", "target_session_attrs")))
			}
		}
	}

	// Validate cpus if provided
	if svc.Cpus != nil {
		errs = append(errs, validateCPUs(svc.Cpus, appendPath(path, "cpus"))...)
	}

	// Validate memory if provided
	if svc.Memory != nil {
		errs = append(errs, validateMemory(svc.Memory, appendPath(path, "memory"))...)
	}

	// Validate orchestrator_opts (service-specific restrictions on top of shared checks)
	errs = append(errs, validateServiceOrchestratorOpts(svc.OrchestratorOpts, appendPath(path, "orchestrator_opts"))...)

	return errs
}

func validateConnectAs(svc *api.ServiceSpec, dbUsers []*api.DatabaseUserSpec, path []string) []error {
	connectAsPath := appendPath(path, "connect_as")
	if svc.ConnectAs == "" {
		return []error{newValidationError(errors.New("connect_as is required"), connectAsPath)}
	}

	for _, u := range dbUsers {
		if u.Username == svc.ConnectAs {
			// For MCP with allow_writes, the connect_as user must be the db owner
			if svc.ServiceType == "mcp" {
				if allowWrites, ok := svc.Config["allow_writes"].(bool); ok && allowWrites {
					if u.DbOwner == nil || !*u.DbOwner {
						err := errors.New("allow_writes requires connect_as to reference a database_users entry with db_owner: true")
						return []error{newValidationError(err, connectAsPath)}
					}
				}
			}
			return nil
		}
	}

	err := fmt.Errorf("connect_as %q does not match any database_users entry", svc.ConnectAs)
	return []error{newValidationError(err, connectAsPath)}
}

func validateMCPServiceConfig(config map[string]any, path []string, isUpdate bool) []error {
	_, errs := database.ParseMCPServiceConfig(config, isUpdate)
	var result []error
	for _, err := range errs {
		result = append(result, newValidationError(err, path))
	}
	return result
}

func validatePostgRESTServiceConfig(config map[string]any, path []string) []error {
	_, errs := database.ParsePostgRESTServiceConfig(config)
	var result []error
	for _, err := range errs {
		result = append(result, newValidationError(err, path))
	}
	return result
}

func validateDatabaseConnection(dc *api.DatabaseConnection, path []string, nodeNames ds.Set[string]) []error {
	var errs []error

	// Validate target_nodes: no duplicates, no empty strings, must exist in spec
	if dc.TargetNodes != nil {
		seen := make(ds.Set[string], len(dc.TargetNodes))
		for i, node := range dc.TargetNodes {
			nodePath := appendPath(path, "target_nodes", arrayIndexPath(i))
			if node == "" {
				errs = append(errs, newValidationError(errors.New("node name must not be empty"), nodePath))
			} else if nodeNames != nil && !nodeNames.Has(node) {
				errs = append(errs, newValidationError(fmt.Errorf("node %q does not exist in the database spec", node), nodePath))
			}
			if seen.Has(node) {
				errs = append(errs, newValidationError(fmt.Errorf("duplicate node name %q", node), nodePath))
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
			errs = append(errs, newValidationError(err, appendPath(path, "target_session_attrs")))
		}
	}

	return errs
}

func validateRAGServiceConfig(config map[string]any, path []string, isUpdate bool) []error {
	_, errs := database.ParseRAGServiceConfig(config, isUpdate)
	var result []error
	for _, err := range errs {
		result = append(result, newValidationError(err, path))
	}
	return result
}

func validateCPUs(value *string, path []string) []error {
	var errs []error

	cpus, err := parseCPUs(value)
	if err != nil {
		errs = append(errs, newValidationError(err, path))
	}
	if cpus != 0 && cpus < 0.001 {
		err := errors.New("cannot be less than 1 millicpu")
		errs = append(errs, newValidationError(err, path))
	}

	return errs
}

func validateMemory(value *string, path []string) []error {
	var errs []error

	_, err := parseBytes(value)
	if err != nil {
		errs = append(errs, newValidationError(err, path))
	}

	return errs
}

func validatePorts(postgresPort, patroniPort *int, path []string) error {
	postgres := utils.FromPointer(postgresPort)
	patroni := utils.FromPointer(patroniPort)

	if postgres > 0 && postgres == patroni {
		return newValidationError(errors.New("postgres and patroni ports must not conflict"), path)
	}

	return nil
}

func validateUsers(users []*api.DatabaseUserSpec, path []string) []error {
	var errs []error

	seenNames := ds.NewSet[string]()
	var hasOwner bool
	for i, user := range users {
		userPath := appendPath(path, arrayIndexPath(i))

		if seenNames.Has(user.Username) {
			err := errors.New("usernames must be unique within a database")
			errs = append(errs, newValidationError(err, userPath))
		}
		if user.DbOwner != nil && *user.DbOwner && hasOwner {
			err := errors.New("cannot have multiple users with db_owner = true")
			errs = append(errs, newValidationError(err, userPath))
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
func validateServicePortConflicts(svc *api.ServiceSpec, path []string, owner servicePortOwnerMap) []error {
	if svc.Port == nil || *svc.Port <= 0 {
		return nil
	}

	var errs []error
	for _, hostID := range svc.HostIds {
		key := hostPort{hostID: string(hostID), port: *svc.Port}
		if prev, exists := owner[key]; exists {
			err := fmt.Errorf("port %d conflicts with service %q on the same host", *svc.Port, prev)
			errs = append(errs, newValidationError(err, appendPath(path, "port")))
		} else {
			owner[key] = string(svc.ServiceID)
		}
	}
	return errs
}

func validateBackupConfig(cfg *api.BackupConfigSpec, path []string) []error {
	var errs []error

	for i, repo := range cfg.Repositories {
		repoPath := appendPath(path, "repositories", arrayIndexPath(i))
		errs = append(errs, validateBackupRepository(repo, repoPath)...)
	}

	return errs
}

func validateRestoreConfig(cfg *api.RestoreConfigSpec, path []string) []error {
	var errs []error

	sourceDbIdPath := appendPath(path, "source_database_id")
	errs = append(errs, validateIdentifier(string(cfg.SourceDatabaseID), sourceDbIdPath))

	repoPath := appendPath(path, "repository")
	errs = append(errs, validateRestoreRepository(cfg.Repository, repoPath)...)

	restoreOptsPath := appendPath(path, "restore_options")
	errs = append(errs, validatePgBackRestOptions(cfg.RestoreOptions, restoreOptsPath)...)

	return errs
}

func validateBackupRepository(cfg *api.BackupRepositorySpec, path []string) []error {
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

func validateRestoreRepository(cfg *api.RestoreRepositorySpec, path []string) []error {
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

func validateRepoProperties(props repoProperties, path []string) []error {
	var errs []error

	id := utils.FromPointer(props.id)
	if id != "" {
		idPath := appendPath(path, "id")
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
		err := newValidationError(
			fmt.Errorf("unsupported repo type '%s'", repoType),
			appendPath(path, "type"),
		)
		errs = append(errs, err)
	}

	customOptsPath := appendPath(path, "custom_options")
	errs = append(errs, validatePgBackRestOptions(props.customOptions, customOptsPath)...)

	return errs
}

func validateAzureRepoProperties(props repoProperties, path []string) []error {
	var errs []error

	if utils.FromPointer(props.azureAccount) == "" {
		err := errors.New("azure_account is required for azure repositories")
		errs = append(errs, newValidationError(err, appendPath(path, "azure_account")))
	}
	if utils.FromPointer(props.azureContainer) == "" {
		err := errors.New("azure_container is required for azure repositories")
		errs = append(errs, newValidationError(err, appendPath(path, "azure_container")))
	}
	if utils.FromPointer(props.azureKey) == "" {
		err := errors.New("azure_key is required for azure repositories")
		errs = append(errs, newValidationError(err, appendPath(path, "azure_key")))
	}

	return errs
}

func validateFSRepoProperties(props repoProperties, path []string) []error {
	var errs []error

	basePath := utils.FromPointer(props.basePath)
	if basePath == "" {
		err := fmt.Errorf("base_path is required for %s repositories", props.repoType)
		errs = append(errs, newValidationError(err, appendPath(path, "base_path")))
	} else if !filepath.IsAbs(*props.basePath) {
		err := fmt.Errorf("base_path must be absolute for %s repositories", props.repoType)
		errs = append(errs, newValidationError(err, appendPath(path, "base_path")))
	}

	return errs
}

func validateGCSRepoProperties(props repoProperties, path []string) []error {
	var errs []error

	if utils.FromPointer(props.gcsBucket) == "" {
		err := errors.New("gcs_bucket is required for gcs repositories")
		errs = append(errs, newValidationError(err, appendPath(path, "gcs_bucket")))
	}

	return errs
}

func validateS3RepoProperties(props repoProperties, path []string) []error {
	var errs []error

	if utils.FromPointer(props.s3Bucket) == "" {
		err := errors.New("s3_bucket is required for s3 repositories")
		errs = append(errs, newValidationError(err, appendPath(path, "s3_bucket")))
	}

	return errs
}

var pgBackRestOptionPattern = regexp.MustCompile(`^[a-z0-9-]+$`)
var semverPattern = regexp.MustCompile(`^\d+\.\d+(\.\d+)?$`)

// reservedLabelPrefix is the label key prefix reserved for system use.
const reservedLabelPrefix = "pgedge."

func validateOrchestratorOpts(opts *api.OrchestratorOpts, path []string) []error {
	if opts == nil || opts.Swarm == nil {
		return nil
	}

	var errs []error
	for key := range opts.Swarm.ExtraLabels {
		if strings.HasPrefix(key, reservedLabelPrefix) {
			labelPath := appendPath(path, "swarm", "extra_labels", mapKeyPath(key))
			err := fmt.Errorf("labels starting with %q are reserved for system use", reservedLabelPrefix)
			errs = append(errs, newValidationError(err, labelPath))
		}
	}
	return errs
}

// validateServiceOrchestratorOpts runs the shared orchestrator_opts checks and
// adds service-specific restrictions. Services do not support extra_volumes
// (bind mounts are configured per service type) or driver_opts on extra_networks.
func validateServiceOrchestratorOpts(opts *api.OrchestratorOpts, path []string) []error {
	errs := validateOrchestratorOpts(opts, path)

	if opts == nil || opts.Swarm == nil {
		return errs
	}

	if len(opts.Swarm.ExtraVolumes) > 0 {
		err := errors.New("extra_volumes is not supported for services")
		errs = append(errs, newValidationError(err, appendPath(path, "swarm", "extra_volumes")))
	}

	for i, net := range opts.Swarm.ExtraNetworks {
		if len(net.DriverOpts) > 0 {
			netPath := appendPath(path, "swarm", "extra_networks", arrayIndexPath(i), "driver_opts")
			err := errors.New("driver_opts is not supported for services")
			errs = append(errs, newValidationError(err, netPath))
		}
	}

	return errs
}

func validatePgBackRestOptions(opts map[string]string, path []string) []error {
	var errs []error

	for key := range opts {
		if !pgBackRestOptionPattern.MatchString(key) {
			optPath := appendPath(path, mapKeyPath(key))
			err := errors.New("invalid option name")
			errs = append(errs, newValidationError(err, optPath))
		}
	}

	return errs
}

func validateBackupOptions(opts *api.BackupOptions) error {
	var errs []error

	optsPath := []string{"backup_options"}
	errs = append(errs, validatePgBackRestOptions(opts.BackupOptions, optsPath)...)

	return errors.Join(errs...)
}

func validateIdentifier(ident string, path []string) error {
	if err := utils.ValidateID(ident); err != nil {
		return newValidationError(err, path)
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

func validateScripts(scripts *api.DatabaseScripts, path []string) []error {
	if scripts == nil {
		return nil
	}
	return slices.Concat(
		validateScript(scripts.PostInit, appendPath(path, "post_init")),
		validateScript(scripts.PostDatabaseCreate, appendPath(path, "post_database_create")),
	)
}

func validateScript(statements []string, path []string) []error {
	var errs []error
	for i, statement := range statements {
		statementPath := appendPath(path, arrayIndexPath(i))
		if err := validateSQLStatement(statement, statementPath); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func validateSQLStatement(statement string, path []string) error {
	_, err := postgresparser.ParseSQLStrict(statement)
	if err != nil {
		err = fmt.Errorf("failed to parse SQL statement: %w", err)
		return newValidationError(err, path)
	}
	return nil
}
