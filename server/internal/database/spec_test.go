package database_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
)

func TestSpec(t *testing.T) {
	port := 5432
	base := &database.Spec{
		PostgresVersion: "17.6",
		SpockVersion:    "5",
		Port:            &port,
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
				ExtraLabels: map[string]string{
					"pgedge.custom.label": "custom-value",
				},
			},
		},
	}

	t.Run("Update", func(t *testing.T) {
		t.Run("no changes", func(t *testing.T) {
			current := base.Clone()
			new := base.Clone()
			// simulating an update request that excluded optional fields
			new.PostgresVersion = ""
			new.SpockVersion = ""
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

			new.DefaultOptionalFieldsFrom(current)

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

			new.DefaultOptionalFieldsFrom(current)

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

			new.DefaultOptionalFieldsFrom(current)

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

			new.DefaultOptionalFieldsFrom(current)

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

			new.DefaultOptionalFieldsFrom(current)

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
							{
								ID:             "azure-backups",
								AzureAccount:   "pgedge-backups",
								AzureContainer: "pgedge-backups-9f81786f-373b-4ff2-afee-e054a06a96f1",
								AzureKey:       "NEWYXpLZXk=",
							},
							{
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
							{
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

			new.DefaultOptionalFieldsFrom(current)

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

			new.DefaultOptionalFieldsFrom(current)

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

			new.DefaultOptionalFieldsFrom(current)

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

			new.DefaultOptionalFieldsFrom(current)

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

			new.DefaultOptionalFieldsFrom(current)

			assert.Equal(t, expected, new)
		})

		t.Run("updating postgres and spock versions", func(t *testing.T) {
			current := base.Clone()
			new := base.Clone()
			new.PostgresVersion = "18.0"
			new.SpockVersion = "6"
			expected := new.Clone()

			new.DefaultOptionalFieldsFrom(current)

			assert.Equal(t, expected, new)
		})
	})
}

func TestSwarmOptsClone(t *testing.T) {
	t.Run("copies Image and ResolvedImage", func(t *testing.T) {
		orig := &database.SwarmOpts{
			Image:         "custom-registry/pgedge:dev",
			ResolvedImage: "registry/pgedge:17.9-spock5.0.6-standard-1",
			ExtraLabels:   map[string]string{"k": "v"},
		}
		cloned := orig.Clone()

		assert.Equal(t, orig.Image, cloned.Image)
		assert.Equal(t, orig.ResolvedImage, cloned.ResolvedImage)
	})

	t.Run("clone is independent of original", func(t *testing.T) {
		orig := &database.SwarmOpts{
			Image:         "original-image",
			ResolvedImage: "original-resolved",
		}
		cloned := orig.Clone()
		cloned.Image = "mutated-image"
		cloned.ResolvedImage = "mutated-resolved"

		assert.Equal(t, "original-image", orig.Image)
		assert.Equal(t, "original-resolved", orig.ResolvedImage)
	})

	t.Run("nil clone returns nil", func(t *testing.T) {
		var s *database.SwarmOpts
		assert.Nil(t, s.Clone())
	})
}

func TestSpec_NodeInstances_DBOwner(t *testing.T) {
	minimalSpec := func(users []*database.User) *database.Spec {
		return &database.Spec{
			DatabaseID:      "test-db",
			DatabaseName:    "testdb",
			PostgresVersion: "17.6",
			SpockVersion:    "5",
			DatabaseUsers:   users,
			Nodes: []*database.Node{
				{Name: "n1", HostIDs: []string{"host-1"}},
			},
		}
	}

	t.Run("single db_owner is propagated", func(t *testing.T) {
		spec := minimalSpec([]*database.User{
			{Username: "app", DBOwner: true},
			{Username: "admin"},
		})
		nodes, err := spec.NodeInstances()
		assert.NoError(t, err)
		assert.Equal(t, "app", nodes[0].DatabaseOwner)
	})

	t.Run("no db_owner results in empty owner", func(t *testing.T) {
		spec := minimalSpec([]*database.User{
			{Username: "app"},
			{Username: "admin"},
		})
		nodes, err := spec.NodeInstances()
		assert.NoError(t, err)
		assert.Empty(t, nodes[0].DatabaseOwner)
	})

	t.Run("multiple db_owners returns error", func(t *testing.T) {
		spec := minimalSpec([]*database.User{
			{Username: "app", DBOwner: true},
			{Username: "other", DBOwner: true},
		})
		_, err := spec.NodeInstances()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "only one user can have db_owner=true")
	})

	t.Run("owner propagates to all nodes", func(t *testing.T) {
		spec := minimalSpec([]*database.User{
			{Username: "app", DBOwner: true},
		})
		spec.Nodes = append(spec.Nodes, &database.Node{Name: "n2", HostIDs: []string{"host-2"}})
		nodes, err := spec.NodeInstances()
		assert.NoError(t, err)
		assert.Len(t, nodes, 2)
		assert.Equal(t, "app", nodes[0].DatabaseOwner)
		assert.Equal(t, "app", nodes[1].DatabaseOwner)
	})
}

func TestSpec_NodeInstances_PgHbaIdentMerge(t *testing.T) {
	dbHba := []string{"host all myapp_user 203.0.113.0/24 scram-sha-256"}
	dbIdent := []string{"ssl_users CN=alice alice"}

	spec := func(n1, n2 *database.Node) *database.Spec {
		return &database.Spec{
			DatabaseID:      "test-db",
			DatabaseName:    "testdb",
			PostgresVersion: "17.6",
			SpockVersion:    "5",
			PgHbaConf:       dbHba,
			PgIdentConf:     dbIdent,
			Nodes:           []*database.Node{n1, n2},
		}
	}

	t.Run("node with no entries inherits database-level list", func(t *testing.T) {
		s := spec(
			&database.Node{Name: "n1", HostIDs: []string{"host-1"}},
			&database.Node{Name: "n2", HostIDs: []string{"host-2"}},
		)
		nodes, err := s.NodeInstances()
		assert.NoError(t, err)
		assert.Equal(t, dbHba, nodes[0].Instances[0].PgHbaConf)
		assert.Equal(t, dbIdent, nodes[0].Instances[0].PgIdentConf)
	})

	t.Run("node entries are prepended ahead of database-level entries", func(t *testing.T) {
		nodeHba := "host example myapp_user 10.0.0.0/8 scram-sha-256"
		s := spec(
			&database.Node{Name: "n1", HostIDs: []string{"host-1"}, PgHbaConf: []string{nodeHba}},
			&database.Node{Name: "n2", HostIDs: []string{"host-2"}},
		)
		nodes, err := s.NodeInstances()
		assert.NoError(t, err)
		// n1: node entry first, then database-level entry (first-match priority).
		assert.Equal(t, append([]string{nodeHba}, dbHba...), nodes[0].Instances[0].PgHbaConf)
		// n2: unchanged, inherits database-level list only.
		assert.Equal(t, dbHba, nodes[1].Instances[0].PgHbaConf)
	})

	t.Run("empty everywhere yields nil", func(t *testing.T) {
		s := &database.Spec{
			DatabaseID:      "test-db",
			DatabaseName:    "testdb",
			PostgresVersion: "17.6",
			SpockVersion:    "5",
			Nodes:           []*database.Node{{Name: "n1", HostIDs: []string{"host-1"}}},
		}
		nodes, err := s.NodeInstances()
		assert.NoError(t, err)
		assert.Nil(t, nodes[0].Instances[0].PgHbaConf)
		assert.Nil(t, nodes[0].Instances[0].PgIdentConf)
	})
}
