package pgbackrest

import (
	"fmt"
	"io"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"

	"gopkg.in/ini.v1"

	"github.com/google/uuid"
)

type RepositoryType string

const (
	RepositoryTypeS3    RepositoryType = "s3"
	RepositoryTypeGCS   RepositoryType = "gcs"
	RepositoryTypeAzure RepositoryType = "azure"
	RepositoryTypePosix RepositoryType = "posix"
	RepositoryTypeCifs  RepositoryType = "cifs"
)

type RetentionFullType string

const (
	RetentionFullTypeTime  RetentionFullType = "time"
	RetentionFullTypeCount RetentionFullType = "count"
)

type Repository struct {
	ID                string            `json:"id"`
	Type              RepositoryType    `json:"type"`
	S3Bucket          string            `json:"s3_bucket,omitempty"`
	S3Region          string            `json:"s3_region,omitempty"`
	S3Endpoint        string            `json:"s3_endpoint,omitempty"`
	S3Key             string            `json:"s3_key,omitempty"`
	S3KeySecret       string            `json:"s3_key_secret,omitempty"`
	GCSBucket         string            `json:"gcs_bucket,omitempty"`
	GCSEndpoint       string            `json:"gcs_endpoint,omitempty"`
	GCSKey            string            `json:"gcs_key,omitempty"`
	AzureAccount      string            `json:"azure_account,omitempty"`
	AzureContainer    string            `json:"azure_container,omitempty"`
	AzureEndpoint     string            `json:"azure_endpoint,omitempty"`
	AzureKey          string            `json:"azure_key,omitempty"`
	RetentionFull     int               `json:"retention_full"`
	RetentionFullType RetentionFullType `json:"retention_full_type"`
	BasePath          string            `json:"base_path,omitempty"`
	CustomOptions     map[string]string `json:"custom_options,omitempty"`
}

func (r *Repository) Clone() *Repository {
	if r == nil {
		return nil
	}
	return &Repository{
		ID:                r.ID,
		Type:              r.Type,
		S3Bucket:          r.S3Bucket,
		S3Region:          r.S3Region,
		S3Endpoint:        r.S3Endpoint,
		S3Key:             r.S3Key,
		S3KeySecret:       r.S3KeySecret,
		GCSBucket:         r.GCSBucket,
		GCSEndpoint:       r.GCSEndpoint,
		GCSKey:            r.GCSKey,
		AzureAccount:      r.AzureAccount,
		AzureContainer:    r.AzureContainer,
		AzureEndpoint:     r.AzureEndpoint,
		AzureKey:          r.AzureKey,
		RetentionFull:     r.RetentionFull,
		RetentionFullType: r.RetentionFullType,
		BasePath:          r.BasePath,
		CustomOptions:     maps.Clone(r.CustomOptions),
	}
}

func (r *Repository) WithDefaults() *Repository {
	out := &Repository{
		ID:                r.ID,
		Type:              r.Type,
		S3Bucket:          r.S3Bucket,
		S3Region:          r.S3Region,
		S3Endpoint:        r.S3Endpoint,
		S3Key:             r.S3Key,
		S3KeySecret:       r.S3KeySecret,
		GCSBucket:         r.GCSBucket,
		GCSEndpoint:       r.GCSEndpoint,
		GCSKey:            r.GCSKey,
		AzureAccount:      r.AzureAccount,
		AzureContainer:    r.AzureContainer,
		AzureEndpoint:     r.AzureEndpoint,
		AzureKey:          r.AzureKey,
		RetentionFull:     r.RetentionFull,
		RetentionFullType: r.RetentionFullType,
		BasePath:          r.BasePath,
		CustomOptions:     r.CustomOptions,
	}
	if out.S3Endpoint == "" {
		if out.S3Region != "" {
			out.S3Endpoint = fmt.Sprintf("s3.%s.amazonaws.com", out.S3Region)
		} else {
			out.S3Endpoint = "s3.amazonaws.com"
		}
	}
	if out.GCSEndpoint == "" {
		out.GCSEndpoint = "storage.googleapis.com"
	}
	if out.AzureEndpoint == "" {
		out.AzureEndpoint = "blob.core.windows.net"
	}
	if out.RetentionFullType == "" {
		out.RetentionFullType = RetentionFullTypeTime
	}
	if out.RetentionFull == 0 {
		out.RetentionFull = 7
	}
	return out
}

type ConfigOptions struct {
	DatabaseID   uuid.UUID
	NodeName     string
	PgDataPath   string
	HostUser     string
	User         string
	SocketPath   string
	Repositories []*Repository
}

func WriteConfig(w io.Writer, opts ConfigOptions) error {
	global := map[string]string{
		"start-fast":        "y",
		"log-level-console": "info",
	}

	for idx, repo := range opts.Repositories {
		repo = repo.WithDefaults()
		global[repoKey(idx, "path")] = repoPath(opts.DatabaseID, repo.BasePath, opts.NodeName, repo.ID)
		global[repoKey(idx, "type")] = string(repo.Type)
		global[repoKey(idx, "cipher-type")] = "none"
		global[repoKey(idx, "retention-full")] = strconv.Itoa(repo.RetentionFull)
		global[repoKey(idx, "retention-full-type")] = string(repo.RetentionFullType)

		switch repo.Type {
		case RepositoryTypeS3:
			writeS3Repo(idx, repo, global)
		case RepositoryTypeGCS:
			writeGCSRepo(idx, repo, global)
		case RepositoryTypeAzure:
			writeAzureRepo(idx, repo, global)
		case RepositoryTypePosix:
			// For Posix repositories, we don't need to add any specific keys
			// to the global section, as the path is already set.
		case RepositoryTypeCifs:
			// For CIFS repositories, just like Posix, we don't need to
			// add any specific keys to the global section, as the path is
			// already set.
		default:
			return fmt.Errorf("unsupported repository type: %q", repo.Type)
		}

		// Custom options written at the end to allow for overrides.
		writeCustomOptions(idx, repo, global)
	}

	db := map[string]string{
		"pg1-path":      opts.PgDataPath,
		"pg1-host-user": opts.HostUser,
		"pg1-user":      opts.User,
	}

	if opts.SocketPath != "" {
		db["pg1-socket-path"] = opts.SocketPath
	}

	file := ini.Empty()

	globalSection, err := file.NewSection("global")
	if err != nil {
		return fmt.Errorf("failed to add 'global' section: %w", err)
	}
	if err := populateSection(globalSection, global); err != nil {
		return fmt.Errorf("failed to populate 'global' section: %w", err)
	}

	dbSection, err := file.NewSection("db")
	if err != nil {
		return fmt.Errorf("failed to add 'db' section: %w", err)
	}
	if err := populateSection(dbSection, db); err != nil {
		return fmt.Errorf("failed to populate 'db' section: %w", err)
	}

	if _, err := file.WriteTo(w); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

func writeS3Repo(idx int, repo *Repository, contents map[string]string) {
	contents[repoKey(idx, "s3-bucket")] = repo.S3Bucket
	contents[repoKey(idx, "s3-endpoint")] = repo.S3Endpoint
	if repo.S3Region != "" {
		contents[repoKey(idx, "s3-region")] = repo.S3Region
	}
	if repo.S3Key != "" {
		contents[repoKey(idx, "s3-key-type")] = "shared"
		contents[repoKey(idx, "s3-key")] = repo.S3Key
		contents[repoKey(idx, "s3-key-secret")] = repo.S3KeySecret
	} else {
		contents[repoKey(idx, "s3-key-type")] = "auto"
	}
}

func writeGCSRepo(idx int, repo *Repository, contents map[string]string) {
	contents[repoKey(idx, "gcs-bucket")] = repo.GCSBucket
	contents[repoKey(idx, "gcs-endpoint")] = repo.GCSEndpoint
	if repo.GCSKey != "" {
		contents[repoKey(idx, "gcs-key-type")] = "shared"
		contents[repoKey(idx, "gcs-key")] = repo.GCSKey
	} else {
		contents[repoKey(idx, "gcs-key-type")] = "auto"
	}
}

func writeAzureRepo(idx int, repo *Repository, contents map[string]string) {
	contents[repoKey(idx, "azure-account")] = repo.AzureAccount
	contents[repoKey(idx, "azure-container")] = repo.AzureContainer
	contents[repoKey(idx, "azure-endpoint")] = repo.AzureEndpoint
	contents[repoKey(idx, "azure-uri-style")] = "host"
	if repo.AzureKey != "" {
		contents[repoKey(idx, "azure-key-type")] = "shared"
		contents[repoKey(idx, "azure-key")] = repo.AzureKey
	} else {
		contents[repoKey(idx, "azure-key-type")] = "auto"
	}
}

var repoPrefixRegex = regexp.MustCompile(`^repo\d+-`)

func writeCustomOptions(idx int, repo *Repository, contents map[string]string) {
	for key, value := range repo.CustomOptions {
		// Sanitize the keys in case someone preemptively added the repoN prefix.
		key = repoPrefixRegex.ReplaceAllString(key, "")
		contents[repoKey(idx, key)] = value
	}
}

func populateSection(section *ini.Section, contents map[string]string) error {
	// We want to sort the keys so that the output is deterministic. This makes
	// testing easier.
	for _, key := range slices.Sorted(maps.Keys(contents)) {
		value := contents[key]
		if _, err := section.NewKey(key, value); err != nil {
			return fmt.Errorf("failed to add key %q: %w", key, err)
		}
	}
	return nil
}

func repoKey(idx int, key string) string {
	return fmt.Sprintf("repo%d-%s", idx+1, key)
}

func repoPath(databaseID uuid.UUID, basePath, nodeName, repoID string) string {
	return path.Join("/", basePath, "databases", databaseID.String(), repoID, nodeName)
}
