package apiv1

import (
	"errors"
	"testing"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/stretchr/testify/assert"
)

func TestValidationError(t *testing.T) {
	t.Run("with path", func(t *testing.T) {
		err := newValidationError(errors.New("test error"), []string{
			"array",
			arrayIndexPath(0),
			"map",
			mapKeyPath("key"),
		})

		assert.ErrorContains(t, err, "array[0].map[key]: test error")
	})

	t.Run("without path", func(t *testing.T) {
		err := newValidationError(errors.New("test error"), nil)

		assert.ErrorContains(t, err, "test error")
	})
}

func TestValidateCPUs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cpus     *string
		expected string
	}{
		{
			name: "nil",
			cpus: nil,
		},
		{
			name: "empty",
			cpus: utils.PointerTo(""),
		},
		{
			name:     "invalid",
			cpus:     utils.PointerTo("%&*^"),
			expected: "failed to parse CPUs",
		},
		{
			name:     "too small",
			cpus:     utils.PointerTo("0.00001"),
			expected: "cannot be less than 1 millicpu",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := errors.Join(validateCPUs(tc.cpus, nil)...)
			if tc.expected == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.expected)
			}
		})
	}
}

func TestValidateMemory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		memory   *string
		expected string
	}{
		{
			name:   "nil",
			memory: nil,
		},
		{
			name:   "empty",
			memory: utils.PointerTo(""),
		},
		{
			name:     "invalid",
			memory:   utils.PointerTo("%&*^"),
			expected: "failed to parse bytes",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := errors.Join(validateMemory(tc.memory, nil)...)
			if tc.expected == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.expected)
			}
		})
	}
}

func TestValidateRepoProperties(t *testing.T) {
	for _, tc := range []struct {
		name     string
		props    repoProperties
		expected []string
	}{
		{
			name: "azure valid",
			props: repoProperties{
				id:             utils.PointerTo(api.Identifier("ed5913d1-f603-4dd6-8c7c-0a28cd2b27d2")),
				repoType:       "azure",
				azureAccount:   utils.PointerTo("my-account"),
				azureContainer: utils.PointerTo("my-container"),
				azureKey:       utils.PointerTo("my-key"),
				customOptions: map[string]string{
					"some-option": "yes",
				},
			},
		},
		{
			name: "azure invalid",
			props: repoProperties{
				id:       utils.PointerTo(api.Identifier("/invalid")),
				repoType: "azure",
				customOptions: map[string]string{
					"/bad": "yes",
				},
			},
			expected: []string{
				"id: valid IDs must",
				"azure_account: azure_account is required for azure repositories",
				"azure_container: azure_container is required for azure repositories",
				"azure_key: azure_key is required for azure repositories",
				"custom_options[/bad]: invalid option name",
			},
		},
		{
			name: "cifs valid",
			props: repoProperties{
				repoType: "cifs",
				basePath: utils.PointerTo("/backups"),
			},
		},
		{
			name: "cifs invalid missing",
			props: repoProperties{
				repoType: "cifs",
			},
			expected: []string{
				"base_path: base_path is required for cifs repositories",
			},
		},
		{
			name: "cifs invalid relative",
			props: repoProperties{
				repoType: "cifs",
				basePath: utils.PointerTo("./backups"),
			},
			expected: []string{
				"base_path: base_path must be absolute for cifs repositories",
			},
		},
		{
			name: "posix valid",
			props: repoProperties{
				repoType: "posix",
				basePath: utils.PointerTo("/backups"),
			},
		},
		{
			name: "posix invalid missing",
			props: repoProperties{
				repoType: "posix",
			},
			expected: []string{
				"base_path: base_path is required for posix repositories",
			},
		},
		{
			name: "posix invalid relative",
			props: repoProperties{
				repoType: "posix",
				basePath: utils.PointerTo("./backups"),
			},
			expected: []string{
				"base_path: base_path must be absolute for posix repositories",
			},
		},
		{
			name: "gcs valid",
			props: repoProperties{
				repoType:  "gcs",
				gcsBucket: utils.PointerTo("my-backups"),
			},
		},
		{
			name: "gcs invalid",
			props: repoProperties{
				repoType: "gcs",
			},
			expected: []string{
				"gcs_bucket: gcs_bucket is required for gcs repositories",
			},
		},
		{
			name: "s3 valid",
			props: repoProperties{
				repoType: "s3",
				s3Bucket: utils.PointerTo("my-backups"),
			},
		},
		{
			name: "s3 invalid",
			props: repoProperties{
				repoType: "s3",
			},
			expected: []string{
				"s3_bucket: s3_bucket is required for s3 repositories",
			},
		},
		{
			name: "unsupported type",
			props: repoProperties{
				repoType: "other",
			},
			expected: []string{
				"type: unsupported repo type 'other'",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := errors.Join(validateRepoProperties(tc.props, nil)...)
			if len(tc.expected) < 1 {
				assert.NoError(t, err)
			} else {
				for _, expected := range tc.expected {
					assert.ErrorContains(t, err, expected)
				}
			}
		})
	}
}

func TestValidateRestoreConfig(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfg      *api.RestoreConfigSpec
		expected []string
	}{
		{
			name: "valid minimal",
			cfg: &api.RestoreConfigSpec{
				SourceDatabaseID: "cd1ca642-4ad7-11f0-9d4d-f76d20f5a13d",
				Repository: &api.RestoreRepositorySpec{
					Type:     "posix",
					BasePath: utils.PointerTo("/backups"),
				},
			},
		},
		{
			name: "valid all",
			cfg: &api.RestoreConfigSpec{
				SourceDatabaseID: "cd1ca642-4ad7-11f0-9d4d-f76d20f5a13d",
				Repository: &api.RestoreRepositorySpec{
					Type:     "posix",
					BasePath: utils.PointerTo("/backups"),
				},
				RestoreOptions: map[string]string{
					"type":   "time",
					"target": "2025-01-01T01:30:00Z",
				},
			},
		},
		{
			name: "invalid",
			cfg: &api.RestoreConfigSpec{
				Repository: &api.RestoreRepositorySpec{
					Type:     "posix",
					BasePath: utils.PointerTo("./backups"),
				},
				RestoreOptions: map[string]string{
					"/foo": "bar",
				},
			},
			expected: []string{
				"source_database_id: valid IDs must",
				"repository.base_path: base_path must be absolute for posix repositories",
				"restore_options[/foo]: invalid option name",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := errors.Join(validateRestoreConfig(tc.cfg, nil)...)
			if len(tc.expected) < 1 {
				assert.NoError(t, err)
			} else {
				for _, expected := range tc.expected {
					assert.ErrorContains(t, err, expected)
				}
			}
		})
	}
}

func TestValidateBackupConfig(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfg      *api.BackupConfigSpec
		expected []string
	}{
		{
			name: "valid",
			cfg: &api.BackupConfigSpec{
				Repositories: []*api.BackupRepositorySpec{
					{
						Type:     "posix",
						BasePath: utils.PointerTo("/backups"),
					},
				},
			},
		},
		{
			name: "invalid",
			cfg: &api.BackupConfigSpec{
				Repositories: []*api.BackupRepositorySpec{
					{
						Type:     "posix",
						BasePath: utils.PointerTo("./backups"),
					},
				},
			},
			expected: []string{
				"repositories[0].base_path: base_path must be absolute for posix repositories",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := errors.Join(validateBackupConfig(tc.cfg, nil)...)
			if len(tc.expected) < 1 {
				assert.NoError(t, err)
			} else {
				for _, expected := range tc.expected {
					assert.ErrorContains(t, err, expected)
				}
			}
		})
	}
}

func TestValidateNode(t *testing.T) {
	for _, tc := range []struct {
		name     string
		node     *api.DatabaseNodeSpec
		expected []string
	}{
		{
			name: "valid minimal",
			node: &api.DatabaseNodeSpec{
				HostIds: []api.Identifier{
					api.Identifier("host-1"),
				},
			},
		},
		{
			name: "valid all",
			node: &api.DatabaseNodeSpec{
				Cpus:   utils.PointerTo("16"),
				Memory: utils.PointerTo("64GiB"),
				HostIds: []api.Identifier{
					api.Identifier("host-1"),
					api.Identifier("host-2"),
					api.Identifier("host-3"),
				},
				BackupConfig: &api.BackupConfigSpec{
					Repositories: []*api.BackupRepositorySpec{
						{
							Type:     "posix",
							BasePath: utils.PointerTo("/backups"),
						},
					},
				},
				RestoreConfig: &api.RestoreConfigSpec{
					SourceDatabaseID: "cd1ca642-4ad7-11f0-9d4d-f76d20f5a13d",
					Repository: &api.RestoreRepositorySpec{
						Type:     "posix",
						BasePath: utils.PointerTo("/backups"),
					},
				},
			},
		},
		{
			name: "invalid",
			node: &api.DatabaseNodeSpec{
				Cpus:   utils.PointerTo("0.00001"),
				Memory: utils.PointerTo("%^&*"),
				HostIds: []api.Identifier{
					api.Identifier("host-1"),
					api.Identifier("host-2"),
					api.Identifier("host.3"),
					api.Identifier("host-1"),
				},
				BackupConfig: &api.BackupConfigSpec{
					Repositories: []*api.BackupRepositorySpec{
						{
							Type:     "posix",
							BasePath: utils.PointerTo("./backups"),
						},
					},
				},
				RestoreConfig: &api.RestoreConfigSpec{
					Repository: &api.RestoreRepositorySpec{
						Type:     "posix",
						BasePath: utils.PointerTo("./backups"),
					},
				},
			},
			expected: []string{
				"cpus: cannot be less than 1 millicpu",
				"memory: failed to parse bytes",
				"host_ids[2]: valid IDs must",
				"host_ids[3]: host IDs must be unique within a node",
				"backup_config.repositories[0].base_path: base_path must be absolute for posix repositories",
				"restore_config.repository.base_path: base_path must be absolute for posix repositories",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := errors.Join(validateNode(tc.node, nil)...)
			if len(tc.expected) < 1 {
				assert.NoError(t, err)
			} else {
				for _, expected := range tc.expected {
					assert.ErrorContains(t, err, expected)
				}
			}
		})
	}
}

func TestValidateDatabaseSpec(t *testing.T) {
	for _, tc := range []struct {
		name     string
		spec     *api.DatabaseSpec
		expected []string
	}{
		{
			name: "valid minimal",
			spec: &api.DatabaseSpec{
				Nodes: []*api.DatabaseNodeSpec{
					{
						Name: "n1",
						HostIds: []api.Identifier{
							api.Identifier("host-1"),
						},
					},
				},
			},
		},
		{
			name: "valid all",
			spec: &api.DatabaseSpec{
				Cpus:   utils.PointerTo("16"),
				Memory: utils.PointerTo("64GiB"),
				Nodes: []*api.DatabaseNodeSpec{
					{
						Name: "n1",
						HostIds: []api.Identifier{
							api.Identifier("host-1"),
						},
					},
					{
						Name: "n2",
						HostIds: []api.Identifier{
							api.Identifier("host-2"),
						},
					},
					{
						Name: "n3",
						HostIds: []api.Identifier{
							api.Identifier("host-3"),
						},
					},
				},
				BackupConfig: &api.BackupConfigSpec{
					Repositories: []*api.BackupRepositorySpec{
						{
							Type:     "posix",
							BasePath: utils.PointerTo("/backups"),
						},
					},
				},
				RestoreConfig: &api.RestoreConfigSpec{
					SourceDatabaseID: "cd1ca642-4ad7-11f0-9d4d-f76d20f5a13d",
					Repository: &api.RestoreRepositorySpec{
						Type:     "posix",
						BasePath: utils.PointerTo("/backups"),
					},
				},
			},
		},
		{
			name: "valid with services",
			spec: &api.DatabaseSpec{
				DatabaseName:    "testdb",
				PostgresVersion: utils.PointerTo("17.6"),
				Nodes: []*api.DatabaseNodeSpec{
					{
						Name: "n1",
						HostIds: []api.Identifier{
							api.Identifier("host-1"),
						},
					},
				},
				Services: []*api.ServiceSpec{
					{
						ServiceID:   "mcp-server",
						ServiceType: "mcp",
						Version:     "1.0.0",
						HostIds:     []api.Identifier{"host-1"},
						Config: map[string]any{
							"llm_provider":      "anthropic",
							"llm_model":         "claude-sonnet-4-5",
							"anthropic_api_key": "sk-ant-...",
						},
					},
				},
			},
		},
		{
			name: "invalid with duplicate service IDs",
			spec: &api.DatabaseSpec{
				DatabaseName:    "testdb",
				PostgresVersion: utils.PointerTo("17.6"),
				Nodes: []*api.DatabaseNodeSpec{
					{
						Name: "n1",
						HostIds: []api.Identifier{
							api.Identifier("host-1"),
						},
					},
				},
				Services: []*api.ServiceSpec{
					{
						ServiceID:   "mcp-server",
						ServiceType: "mcp",
						Version:     "1.0.0",
						HostIds:     []api.Identifier{"host-1"},
						Config: map[string]any{
							"llm_provider":      "anthropic",
							"llm_model":         "claude-sonnet-4-5",
							"anthropic_api_key": "sk-ant-...",
						},
					},
					{
						ServiceID:   "mcp-server",
						ServiceType: "mcp",
						Version:     "1.0.0",
						HostIds:     []api.Identifier{"host-2"},
						Config: map[string]any{
							"llm_provider":      "anthropic",
							"llm_model":         "claude-sonnet-4-5",
							"anthropic_api_key": "sk-ant-...",
						},
					},
				},
			},
			expected: []string{
				"services[1]: service IDs must be unique within a database",
			},
		},
		{
			name: "invalid with service validation errors",
			spec: &api.DatabaseSpec{
				DatabaseName:    "testdb",
				PostgresVersion: utils.PointerTo("17.6"),
				Nodes: []*api.DatabaseNodeSpec{
					{
						Name: "n1",
						HostIds: []api.Identifier{
							api.Identifier("host-1"),
						},
					},
				},
				Services: []*api.ServiceSpec{
					{
						ServiceID:   "mcp-server",
						ServiceType: "unknown",
						Version:     "v1.0",
						HostIds:     []api.Identifier{"host-1"},
						Config: map[string]any{
							"llm_provider": "unknown",
						},
					},
				},
			},
			expected: []string{
				"services[0].service_type: unsupported service type 'unknown'",
				"services[0].version: version must be in semver format (e.g., '1.0.0') or 'latest'",
			},
		},
		{
			name: "invalid with MCP config errors",
			spec: &api.DatabaseSpec{
				DatabaseName:    "testdb",
				PostgresVersion: utils.PointerTo("17.6"),
				Nodes: []*api.DatabaseNodeSpec{
					{
						Name: "n1",
						HostIds: []api.Identifier{
							api.Identifier("host-1"),
						},
					},
				},
				Services: []*api.ServiceSpec{
					{
						ServiceID:   "mcp-server",
						ServiceType: "mcp",
						Version:     "1.0.0",
						HostIds:     []api.Identifier{"host-1"},
						Config: map[string]any{
							"llm_provider": "unknown",
						},
					},
				},
			},
			expected: []string{
				"services[0].config: missing required field 'llm_model'",
				"services[0].config[llm_provider]: unsupported llm_provider 'unknown'",
			},
		},
		{
			name: "invalid",
			spec: &api.DatabaseSpec{
				Cpus:   utils.PointerTo("0.00001"),
				Memory: utils.PointerTo("%^&*"),
				Nodes: []*api.DatabaseNodeSpec{
					{
						Name: "n1",
						HostIds: []api.Identifier{
							api.Identifier("host-1"),
							api.Identifier("host-1"),
						},
					},
					{
						Name: "n2",
						HostIds: []api.Identifier{
							api.Identifier("host-2"),
						},
					},
					{
						Name: "n1",
						HostIds: []api.Identifier{
							api.Identifier("host-3"),
						},
					},
				},
				BackupConfig: &api.BackupConfigSpec{
					Repositories: []*api.BackupRepositorySpec{
						{
							Type:     "posix",
							BasePath: utils.PointerTo("./backups"),
						},
					},
				},
				RestoreConfig: &api.RestoreConfigSpec{
					Repository: &api.RestoreRepositorySpec{
						Type:     "posix",
						BasePath: utils.PointerTo("./backups"),
					},
				},
			},
			expected: []string{
				"cpus: cannot be less than 1 millicpu",
				"memory: failed to parse bytes",
				"nodes[0].host_ids[1]: host IDs must be unique within a node",
				"nodes[2]: node names must be unique within a database",
				"backup_config.repositories[0].base_path: base_path must be absolute for posix repositories",
				"restore_config.repository.base_path: base_path must be absolute for posix repositories",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDatabaseSpec(tc.spec)
			if len(tc.expected) < 1 {
				assert.NoError(t, err)
			} else {
				for _, expected := range tc.expected {
					assert.ErrorContains(t, err, expected)
				}
			}
		})
	}
}

func TestValidateServiceSpec(t *testing.T) {
	for _, tc := range []struct {
		name     string
		svc      *api.ServiceSpec
		expected []string
	}{
		{
			name: "valid MCP service with Anthropic",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "1.0.0",
				HostIds:     []api.Identifier{"host-1", "host-2"},
				Config: map[string]any{
					"llm_provider":      "anthropic",
					"llm_model":         "claude-sonnet-4-5",
					"anthropic_api_key": "sk-ant-...",
				},
			},
		},
		{
			name: "valid MCP service with OpenAI",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "2.1.3",
				HostIds:     []api.Identifier{"host-1"},
				Config: map[string]any{
					"llm_provider":   "openai",
					"llm_model":      "gpt-4",
					"openai_api_key": "sk-...",
				},
			},
		},
		{
			name: "valid MCP service with Ollama",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "1.5.0",
				HostIds:     []api.Identifier{"host-1"},
				Config: map[string]any{
					"llm_provider": "ollama",
					"llm_model":    "llama2",
					"ollama_url":   "http://localhost:11434",
				},
			},
		},
		{
			name: "valid MCP service with 'latest' version",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "latest",
				HostIds:     []api.Identifier{"host-1"},
				Config: map[string]any{
					"llm_provider":      "anthropic",
					"llm_model":         "claude-sonnet-4-5",
					"anthropic_api_key": "sk-ant-...",
				},
			},
		},
		{
			name: "valid MCP service with CPUs and Memory",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "1.0.0",
				HostIds:     []api.Identifier{"host-1"},
				Config: map[string]any{
					"llm_provider":      "anthropic",
					"llm_model":         "claude-sonnet-4-5",
					"anthropic_api_key": "sk-ant-...",
				},
				Cpus:   utils.PointerTo("2"),
				Memory: utils.PointerTo("1GiB"),
			},
		},
		{
			name: "invalid service_id",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp server",
				ServiceType: "mcp",
				Version:     "1.0.0",
				HostIds:     []api.Identifier{"host-1"},
				Config: map[string]any{
					"llm_provider":      "anthropic",
					"llm_model":         "claude-sonnet-4-5",
					"anthropic_api_key": "sk-ant-...",
				},
			},
			expected: []string{
				"service_id:",
			},
		},
		{
			name: "unsupported service_type",
			svc: &api.ServiceSpec{
				ServiceID:   "my-service",
				ServiceType: "unknown",
				Version:     "1.0.0",
				HostIds:     []api.Identifier{"host-1"},
				Config:      map[string]any{},
			},
			expected: []string{
				"service_type: unsupported service type 'unknown'",
			},
		},
		{
			name: "invalid version format",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "v1.0",
				HostIds:     []api.Identifier{"host-1"},
				Config: map[string]any{
					"llm_provider":      "anthropic",
					"llm_model":         "claude-sonnet-4-5",
					"anthropic_api_key": "sk-ant-...",
				},
			},
			expected: []string{
				"version: version must be in semver format (e.g., '1.0.0') or 'latest'",
			},
		},
		{
			name: "duplicate host_ids",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "1.0.0",
				HostIds:     []api.Identifier{"host-1", "host-1"},
				Config: map[string]any{
					"llm_provider":      "anthropic",
					"llm_model":         "claude-sonnet-4-5",
					"anthropic_api_key": "sk-ant-...",
				},
			},
			expected: []string{
				"host_ids[1]: host IDs must be unique within a service",
			},
		},
		{
			name: "invalid host_id",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "1.0.0",
				HostIds:     []api.Identifier{"host 1"},
				Config: map[string]any{
					"llm_provider":      "anthropic",
					"llm_model":         "claude-sonnet-4-5",
					"anthropic_api_key": "sk-ant-...",
				},
			},
			expected: []string{
				"host_ids[0]:",
			},
		},
		{
			name: "missing llm_provider",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "1.0.0",
				HostIds:     []api.Identifier{"host-1"},
				Config: map[string]any{
					"llm_model": "claude-sonnet-4-5",
				},
			},
			expected: []string{
				"config: missing required field 'llm_provider'",
			},
		},
		{
			name: "missing llm_model",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "1.0.0",
				HostIds:     []api.Identifier{"host-1"},
				Config: map[string]any{
					"llm_provider": "anthropic",
				},
			},
			expected: []string{
				"config: missing required field 'llm_model'",
			},
		},
		{
			name: "unsupported llm_provider",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "1.0.0",
				HostIds:     []api.Identifier{"host-1"},
				Config: map[string]any{
					"llm_provider": "unknown",
					"llm_model":    "some-model",
				},
			},
			expected: []string{
				"config[llm_provider]: unsupported llm_provider 'unknown'",
			},
		},
		{
			name: "missing anthropic_api_key",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "1.0.0",
				HostIds:     []api.Identifier{"host-1"},
				Config: map[string]any{
					"llm_provider": "anthropic",
					"llm_model":    "claude-sonnet-4-5",
				},
			},
			expected: []string{
				"config: missing required field 'anthropic_api_key'",
			},
		},
		{
			name: "missing openai_api_key",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "1.0.0",
				HostIds:     []api.Identifier{"host-1"},
				Config: map[string]any{
					"llm_provider": "openai",
					"llm_model":    "gpt-4",
				},
			},
			expected: []string{
				"config: missing required field 'openai_api_key'",
			},
		},
		{
			name: "missing ollama_url",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "1.0.0",
				HostIds:     []api.Identifier{"host-1"},
				Config: map[string]any{
					"llm_provider": "ollama",
					"llm_model":    "llama2",
				},
			},
			expected: []string{
				"config: missing required field 'ollama_url'",
			},
		},
		{
			name: "invalid cpus",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "1.0.0",
				HostIds:     []api.Identifier{"host-1"},
				Config: map[string]any{
					"llm_provider":      "anthropic",
					"llm_model":         "claude-sonnet-4-5",
					"anthropic_api_key": "sk-ant-...",
				},
				Cpus: utils.PointerTo("invalid"),
			},
			expected: []string{
				"cpus: failed to parse CPUs",
			},
		},
		{
			name: "invalid memory",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     "1.0.0",
				HostIds:     []api.Identifier{"host-1"},
				Config: map[string]any{
					"llm_provider":      "anthropic",
					"llm_model":         "claude-sonnet-4-5",
					"anthropic_api_key": "sk-ant-...",
				},
				Memory: utils.PointerTo("invalid"),
			},
			expected: []string{
				"memory: failed to parse bytes",
			},
		},
		{
			name: "multiple validation errors",
			svc: &api.ServiceSpec{
				ServiceID:   "mcp server",
				ServiceType: "unknown",
				Version:     "v1.0",
				HostIds:     []api.Identifier{"host-1", "host-1"},
				Config:      map[string]any{},
				Cpus:        utils.PointerTo("invalid"),
			},
			expected: []string{
				"service_id:",
				"service_type: unsupported service type",
				"version: version must be in semver format (e.g., '1.0.0') or 'latest'",
				"host_ids[1]: host IDs must be unique",
				"cpus: failed to parse CPUs",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := errors.Join(validateServiceSpec(tc.svc, nil)...)
			if len(tc.expected) < 1 {
				assert.NoError(t, err)
			} else {
				for _, expected := range tc.expected {
					assert.ErrorContains(t, err, expected)
				}
			}
		})
	}
}
