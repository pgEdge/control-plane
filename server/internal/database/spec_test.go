package database_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
)

func TestSpec(t *testing.T) {
	base := &database.Spec{
		PostgresVersion: "17",
		SpockVersion:    "4",
		Port:            5432,
		CPUs:            0.5,
		MemoryBytes:     1024 * 1024 * 1024,
		Nodes: []*database.Node{
			{
				Name:    "n1",
				HostIDs: []string{"host-1"},
			},
			{
				Name:    "n2",
				HostIDs: []string{"host-2"},
				BackupConfig: &database.BackupConfig{
					Repositories: []*pgbackrest.Repository{
						{
							ID:             "azure-backups",
							AzureAccount:   "pgedge-backups",
							AzureContainer: "pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
							AzureKey:       "YXpLZXk=",
						},
						{
							ID:             "google-backups",
							AzureAccount:   "pgedge-backups",
							AzureContainer: "pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
							AzureKey:       "ZXhhbXBsZSBnY3Mga2V5Cg==",
						},
					},
				},
			},
			{
				Name:    "n3",
				HostIDs: []string{"host-3"},
				BackupConfig: &database.BackupConfig{
					Repositories: []*pgbackrest.Repository{
						{
							ID:             "google-backups",
							AzureAccount:   "pgedge-backups",
							AzureContainer: "pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
							AzureKey:       "ZXhhbXBsZSBnY3Mga2V5Cg==",
						},
					},
				},
			},
		},
		BackupConfig: &database.BackupConfig{
			Repositories: []*pgbackrest.Repository{
				{
					ID:          "aws-backups",
					Type:        "s3",
					S3Bucket:    "pgedge-backups",
					S3Region:    "us-east-1",
					S3Endpoint:  "s3.us-east-1.amazonaws.com",
					S3Key:       "AKIAIOSFODNN7EXAMPLE",
					S3KeySecret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				},
			},
		},
		DatabaseUsers: []*database.User{
			{
				Username:   "admin",
				Password:   "admin-password",
				DBOwner:    true,
				Attributes: []string{"LOGIN", "SUPERUSER"},
			},
			{
				Username:   "app",
				Password:   "app-password",
				Attributes: []string{"LOGIN"},
				Roles:      []string{"pgedge_application"},
			},
		},
		PostgreSQLConf: map[string]any{
			"max_connections": 1000,
		},
		OrchestratorOpts: &database.OrchestratorOpts{
			Swarm: &database.SwarmOpts{
				ExtraVolumes: []database.ExtraVolumesSpec{
					{
						HostPath:        "/mnt/backups",
						DestinationPath: "/backups",
					},
				},
				ExtraNetworks: []database.ExtraNetworkSpec{
					{
						ID:      "node-network-n1",
						Aliases: []string{"n1-alias"},
					},
				},
			},
		},
	}

	t.Run("Update", func(t *testing.T) {
		t.Run("no changes", func(t *testing.T) {
			current := base.Clone()
			new := base.Clone()
			// simulating an update request that excluded sensitive fields
			new.DatabaseUsers = []*database.User{
				{
					Username:   "admin",
					DBOwner:    true,
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username:   "app",
					Attributes: []string{"LOGIN"},
					Roles:      []string{"pgedge_application"},
				},
			}
			new.Nodes = []*database.Node{
				{
					Name:    "n1",
					HostIDs: []string{"host-1"},
				},
				{
					Name:    "n2",
					HostIDs: []string{"host-2"},
					BackupConfig: &database.BackupConfig{
						Repositories: []*pgbackrest.Repository{
							{
								ID:             "azure-backups",
								AzureAccount:   "pgedge-backups",
								AzureContainer: "pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
							},
							{
								ID:             "google-backups",
								AzureAccount:   "pgedge-backups",
								AzureContainer: "pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
							},
						},
					},
				},
				{
					Name:    "n3",
					HostIDs: []string{"host-3"},
					BackupConfig: &database.BackupConfig{
						Repositories: []*pgbackrest.Repository{
							{
								ID:             "google-backups",
								AzureAccount:   "pgedge-backups",
								AzureContainer: "pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
							},
						},
					},
				},
			}
			new.BackupConfig = &database.BackupConfig{
				Repositories: []*pgbackrest.Repository{
					{
						ID:         "aws-backups",
						Type:       "s3",
						S3Bucket:   "pgedge-backups",
						S3Region:   "us-east-1",
						S3Endpoint: "s3.us-east-1.amazonaws.com",
					},
				},
			}
			expected := base.Clone()

			new.DefaultSensitiveFieldsFrom(current)

			assert.Equal(t, expected, new)
		})
		t.Run("updating a password", func(t *testing.T) {
			current := base.Clone()
			new := base.Clone()
			// simulating an update request that sets a new password for one
			// user
			new.DatabaseUsers = []*database.User{
				{
					Username:   "admin",
					Password:   "new-password",
					DBOwner:    true,
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username:   "app",
					Attributes: []string{"LOGIN"},
					Roles:      []string{"pgedge_application"},
				},
			}
			expected := base.Clone()
			expected.DatabaseUsers = []*database.User{
				{
					Username:   "admin",
					Password:   "new-password",
					DBOwner:    true,
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username:   "app",
					Password:   "app-password",
					Attributes: []string{"LOGIN"},
					Roles:      []string{"pgedge_application"},
				},
			}

			new.DefaultSensitiveFieldsFrom(current)

			assert.Equal(t, expected, new)
		})
		t.Run("adding a user", func(t *testing.T) {
			current := base.Clone()
			new := base.Clone()
			new.DatabaseUsers = []*database.User{
				{
					Username:   "admin",
					DBOwner:    true,
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username:   "app",
					Attributes: []string{"LOGIN"},
					Roles:      []string{"pgedge_application"},
				},
				{
					Username:   "app_read_only",
					Password:   "app-read-only-password",
					Attributes: []string{"LOGIN"},
					Roles:      []string{"pgedge_application_read_only"},
				},
			}
			expected := base.Clone()
			expected.DatabaseUsers = []*database.User{
				{
					Username:   "admin",
					Password:   "admin-password",
					DBOwner:    true,
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
				{
					Username:   "app",
					Password:   "app-password",
					Attributes: []string{"LOGIN"},
					Roles:      []string{"pgedge_application"},
				},
				{
					Username:   "app_read_only",
					Password:   "app-read-only-password",
					Attributes: []string{"LOGIN"},
					Roles:      []string{"pgedge_application_read_only"},
				},
			}

			new.DefaultSensitiveFieldsFrom(current)

			assert.Equal(t, expected, new)
		})
		t.Run("removing a user", func(t *testing.T) {
			current := base.Clone()
			new := base.Clone()
			new.DatabaseUsers = []*database.User{
				{
					Username:   "admin",
					DBOwner:    true,
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			}
			expected := base.Clone()
			expected.DatabaseUsers = []*database.User{
				{
					Username:   "admin",
					Password:   "admin-password",
					DBOwner:    true,
					Attributes: []string{"LOGIN", "SUPERUSER"},
				},
			}

			new.DefaultSensitiveFieldsFrom(current)

			assert.Equal(t, expected, new)
		})
		t.Run("adding backup configs", func(t *testing.T) {
			current := base.Clone()
			current.BackupConfig = nil
			current.Nodes = []*database.Node{
				{
					Name:    "n1",
					HostIDs: []string{"host-1"},
				},
				{
					Name:    "n2",
					HostIDs: []string{"host-2"},
				},
				{
					Name:    "n3",
					HostIDs: []string{"host-3"},
				},
			}
			new := base.Clone()
			expected := base.Clone()

			new.DefaultSensitiveFieldsFrom(current)

			assert.Equal(t, expected, new)
		})
		t.Run("updating backup configs", func(t *testing.T) {
			current := base.Clone()
			new := base.Clone()
			new.BackupConfig = &database.BackupConfig{
				Repositories: []*pgbackrest.Repository{
					{
						ID:          "aws-backups",
						Type:        "s3",
						S3Bucket:    "pgedge-backups",
						S3Region:    "us-east-1",
						S3Endpoint:  "s3.us-east-1.amazonaws.com",
						S3KeySecret: "NEWwJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					},
				},
			}
			new.Nodes = []*database.Node{
				{
					Name:    "n1",
					HostIDs: []string{"host-1"},
				},
				{
					Name:    "n2",
					HostIDs: []string{"host-2"},
					BackupConfig: &database.BackupConfig{
						Repositories: []*pgbackrest.Repository{
							&pgbackrest.Repository{
								ID:             "azure-backups",
								AzureAccount:   "pgedge-backups",
								AzureContainer: "pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
								AzureKey:       "NEWYXpLZXk=",
							},
							&pgbackrest.Repository{
								ID:             "google-backups",
								AzureAccount:   "pgedge-backups",
								AzureContainer: "pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
								AzureKey:       "NEWZXhhbXBsZSBnY3Mga2V5Cg==",
							},
						},
					},
				},
				{
					Name:    "n3",
					HostIDs: []string{"host-3"},
					BackupConfig: &database.BackupConfig{
						Repositories: []*pgbackrest.Repository{
							&pgbackrest.Repository{
								ID:             "google-backups",
								AzureAccount:   "pgedge-backups",
								AzureContainer: "pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
								AzureKey:       "NEWZXhhbXBsZSBnY3Mga2V5Cg==",
							},
						},
					},
				},
			}
			expected := new.Clone()
			expected.BackupConfig = &database.BackupConfig{
				Repositories: []*pgbackrest.Repository{
					{
						ID:          "aws-backups",
						Type:        "s3",
						S3Bucket:    "pgedge-backups",
						S3Region:    "us-east-1",
						S3Endpoint:  "s3.us-east-1.amazonaws.com",
						S3Key:       "AKIAIOSFODNN7EXAMPLE",
						S3KeySecret: "NEWwJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					},
				},
			}

			new.DefaultSensitiveFieldsFrom(current)

			assert.Equal(t, expected, new)
		})
		t.Run("removing backup configs", func(t *testing.T) {
			current := base.Clone()
			new := base.Clone()
			new.BackupConfig = nil
			new.Nodes = []*database.Node{
				{
					Name:    "n1",
					HostIDs: []string{"host-1"},
				},
				{
					Name:    "n2",
					HostIDs: []string{"host-2"},
				},
				{
					Name:    "n3",
					HostIDs: []string{"host-3"},
				},
			}
			expected := new.Clone()

			new.DefaultSensitiveFieldsFrom(current)

			assert.Equal(t, expected, new)
		})
		t.Run("adding restore config", func(t *testing.T) {
			current := base.Clone()
			new := base.Clone()
			new.RestoreConfig = &database.RestoreConfig{
				SourceDatabaseID:   "my-old-app",
				SourceNodeName:     "n1",
				SourceDatabaseName: "my_app",
				Repository: &pgbackrest.Repository{
					ID:          "aws-backups",
					Type:        "s3",
					S3Bucket:    "pgedge-backups",
					S3Region:    "us-east-1",
					S3Endpoint:  "s3.us-east-1.amazonaws.com",
					S3Key:       "AKIAIOSFODNN7EXAMPLE",
					S3KeySecret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				},
			}
			expected := new.Clone()

			new.DefaultSensitiveFieldsFrom(current)

			assert.Equal(t, expected, new)
		})
		t.Run("updating restore config", func(t *testing.T) {
			current := base.Clone()
			current.RestoreConfig = &database.RestoreConfig{
				SourceDatabaseID:   "my-old-app",
				SourceNodeName:     "n1",
				SourceDatabaseName: "my_app",
				Repository: &pgbackrest.Repository{
					ID:          "aws-backups",
					Type:        "s3",
					S3Bucket:    "pgedge-backups",
					S3Region:    "us-east-1",
					S3Endpoint:  "s3.us-east-1.amazonaws.com",
					S3Key:       "AKIAIOSFODNN7EXAMPLE",
					S3KeySecret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				},
			}
			new := current.Clone()
			new.RestoreConfig = &database.RestoreConfig{
				SourceDatabaseID:   "my-old-app",
				SourceNodeName:     "n1",
				SourceDatabaseName: "my_app",
				Repository: &pgbackrest.Repository{
					ID:          "aws-backups",
					Type:        "s3",
					S3Bucket:    "pgedge-backups",
					S3Region:    "us-east-1",
					S3Endpoint:  "s3.us-east-1.amazonaws.com",
					S3Key:       "AKIAIOSFODNN7EXAMPLE",
					S3KeySecret: "NEWwJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				},
			}
			expected := new.Clone()

			new.DefaultSensitiveFieldsFrom(current)

			assert.Equal(t, expected, new)
		})
		t.Run("removing restore config", func(t *testing.T) {
			current := base.Clone()
			current.RestoreConfig = &database.RestoreConfig{
				SourceDatabaseID:   "my-old-app",
				SourceNodeName:     "n1",
				SourceDatabaseName: "my_app",
				Repository: &pgbackrest.Repository{
					ID:          "aws-backups",
					Type:        "s3",
					S3Bucket:    "pgedge-backups",
					S3Region:    "us-east-1",
					S3Endpoint:  "s3.us-east-1.amazonaws.com",
					S3Key:       "AKIAIOSFODNN7EXAMPLE",
					S3KeySecret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				},
			}
			new := base.Clone()
			expected := base.Clone()

			new.DefaultSensitiveFieldsFrom(current)

			assert.Equal(t, expected, new)
		})
	})
}
