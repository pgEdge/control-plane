package apiv1

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
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

func validateDatabaseSpec(spec *api.DatabaseSpec) error {
	var errs []error

	errs = append(errs, validateCPUs(spec.Cpus, []string{"cpus"})...)
	errs = append(errs, validateMemory(spec.Memory, []string{"memory"})...)

	seenNodeNames := make(ds.Set[string], len(spec.Nodes))
	for i, node := range spec.Nodes {
		nodePath := []string{"nodes", arrayIndexPath(i)}

		if seenNodeNames.Has(node.Name) {
			err := errors.New("node names must be unique within a database")
			errs = append(errs, newValidationError(err, nodePath))
		}

		seenNodeNames.Add(node.Name)

		errs = append(errs, validateNode(node, nodePath)...)
	}

	if spec.BackupConfig != nil {
		errs = append(errs, validateBackupConfig(spec.BackupConfig, []string{"backup_config"})...)
	}
	if spec.RestoreConfig != nil {
		errs = append(errs, validateRestoreConfig(spec.RestoreConfig, []string{"restore_config"})...)
	}

	// Validate cross-node source_node references
	errs = append(errs, validateSourceNodeRefs(spec)...)

	return errors.Join(errs...)
}

func validateNode(node *api.DatabaseNodeSpec, path []string) []error {
	var errs []error

	cpusPath := appendPath(path, "cpus")
	errs = append(errs, validateCPUs(node.Cpus, cpusPath)...)

	memPath := appendPath(path, "memory")
	errs = append(errs, validateMemory(node.Memory, memPath)...)

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

	if node.BackupConfig != nil {
		backupConfigPath := appendPath(path, "backup_config")
		errs = append(errs, validateBackupConfig(node.BackupConfig, backupConfigPath)...)
	}
	if node.RestoreConfig != nil {
		restoreConfigPath := appendPath(path, "restore_config")
		errs = append(errs, validateRestoreConfig(node.RestoreConfig, restoreConfigPath)...)
	}

	return errs
}

// validateSourceNodeRefs ensures any provided source_node is valid.
func validateSourceNodeRefs(spec *api.DatabaseSpec) []error {
	var errs []error

	// Collect all node names for membership checks.
	allNames := make(ds.Set[string], len(spec.Nodes))
	for _, n := range spec.Nodes {
		allNames.Add(n.Name)
	}

	for _, n := range spec.Nodes {
		src := utils.FromPointer(n.SourceNode)
		if src == "" {
			continue
		}

		// Self-reference → invalid
		if src == n.Name {
			errs = append(errs, newValidationError(errors.New("invalid source node"), nil))
			continue
		}

		// Unknown reference → "source node dont exist"
		if !allNames.Has(src) {
			errs = append(errs, newValidationError(errors.New("invalid source node"), nil))
		}
	}

	return errs
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
