package database_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

func TestValidateChangedSpec(t *testing.T) {
	for _, tc := range []struct {
		name        string
		current     *database.Spec
		updated     *database.Spec
		expectedErr string
	}{
		{
			name: "no change",
			current: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "test",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
				},
			},
			updated: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "test",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
				},
			},
		},
		{
			name: "valid new instance with new version",
			current: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "test",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
				},
			},
			updated: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "test",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
					{Name: "n2", HostIDs: []string{"host-2"}, PostgresVersion: "18.0"},
				},
			},
		},
		{
			name: "valid database-level version change",
			current: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "test",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
				},
			},
			updated: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "test",
				PostgresVersion: "18.1",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
				},
			},
		},
		{
			name: "valid node-level version change",
			current: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "test",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
					{Name: "n2", HostIDs: []string{"host-2"}},
				},
			},
			updated: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "test",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}, PostgresVersion: "18.1"},
					{Name: "n2", HostIDs: []string{"host-2"}},
				},
			},
		},
		{
			name: "invalid tenant id update",
			current: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "test",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
				},
			},
			updated: &database.Spec{
				TenantID:        utils.PointerTo("new-tenant-id"),
				DatabaseName:    "test",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
				},
			},
			expectedErr: "tenant ID cannot be changed",
		},
		{
			name: "invalid database name update",
			current: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "test",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
				},
			},
			updated: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "updated_test",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
				},
			},
			expectedErr: "database name cannot be changed",
		},
		{
			name: "invalid database-level version update",
			current: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "test",
				PostgresVersion: "17.6",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
				},
			},
			updated: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "updated_test",
				PostgresVersion: "16.10",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
				},
			},
			expectedErr: "major version changed from 17 to 16",
		},
		{
			name: "invalid node-level version update",
			current: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "test",
				PostgresVersion: "17.6",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
					{Name: "n2", HostIDs: []string{"host-2"}},
				},
			},
			updated: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "updated_test",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}, PostgresVersion: "18.0"},
					{Name: "n2", HostIDs: []string{"host-2"}},
				},
			},
			expectedErr: "major version changed from 17 to 18",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := database.ValidateChangedSpec(tc.current, tc.updated)
			if tc.expectedErr != "" {
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateCloneConfig(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)

	// Seed source database into the store so GetDatabase can find it.
	seedSourceDatabase := func(t *testing.T, root, sourceDatabaseID, sourceNodeName, sourceHostID string) {
		t.Helper()
		client := server.Client(t)
		store := database.NewStore(client, root)
		ctx := t.Context()

		err := store.Database.Create(&database.StoredDatabase{
			DatabaseID: sourceDatabaseID,
			State:      database.DatabaseStateAvailable,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}).Exec(ctx)
		require.NoError(t, err)

		err = store.Spec.Create(&database.StoredSpec{
			Spec: &database.Spec{
				DatabaseID:      sourceDatabaseID,
				DatabaseName:    "source-db",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{
						Name:    sourceNodeName,
						HostIDs: []string{sourceHostID},
					},
				},
			},
		}).Exec(ctx)
		require.NoError(t, err)
	}

	for _, tc := range []struct {
		name        string
		cfg         config.Config
		spec        *database.Spec
		seedDB      bool
		expectedErr string
	}{
		{
			name: "single-node required",
			cfg:  config.Config{ZFS: config.ZFS{Enabled: true, Pool: "tank"}},
			spec: &database.Spec{
				DatabaseID:      "clone-db-id",
				DatabaseName:    "clone-db",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{
						Name:    "n1",
						HostIDs: []string{"host-1"},
						CloneConfig: &database.CloneConfig{
							SourceDatabaseID: "source-db-id",
							SourceNodeName:   "source-n1",
						},
					},
					{
						Name:    "n2",
						HostIDs: []string{"host-2"},
					},
				},
			},
			expectedErr: "clone databases must be single-node",
		},
		{
			name: "source database not found",
			cfg:  config.Config{ZFS: config.ZFS{Enabled: true, Pool: "tank"}},
			spec: &database.Spec{
				DatabaseID:      "clone-db-id",
				DatabaseName:    "clone-db",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{
						Name:    "n1",
						HostIDs: []string{"host-1"},
						CloneConfig: &database.CloneConfig{
							SourceDatabaseID: "nonexistent-db-id",
							SourceNodeName:   "source-n1",
						},
					},
				},
			},
			expectedErr: "source database nonexistent-db-id not found",
		},
		{
			name:   "source node not found in source database",
			cfg:    config.Config{ZFS: config.ZFS{Enabled: true, Pool: "tank"}},
			seedDB: true,
			spec: &database.Spec{
				DatabaseID:      "clone-db-id",
				DatabaseName:    "clone-db",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{
						Name:    "n1",
						HostIDs: []string{"host-1"},
						CloneConfig: &database.CloneConfig{
							SourceDatabaseID: "source-db-id",
							SourceNodeName:   "nonexistent-node",
						},
					},
				},
			},
			expectedErr: "source node nonexistent-node not found in database source-db-id",
		},
		{
			name:   "clone on different host than source",
			cfg:    config.Config{ZFS: config.ZFS{Enabled: true, Pool: "tank"}},
			seedDB: true,
			spec: &database.Spec{
				DatabaseID:      "clone-db-id",
				DatabaseName:    "clone-db",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{
						Name:    "n1",
						HostIDs: []string{"different-host"},
						CloneConfig: &database.CloneConfig{
							SourceDatabaseID: "source-db-id",
							SourceNodeName:   "source-n1",
						},
					},
				},
			},
			expectedErr: "clone must be on the same host as source",
		},
		{
			name:   "ZFS not enabled",
			cfg:    config.Config{ZFS: config.ZFS{Enabled: false}},
			seedDB: true,
			spec: &database.Spec{
				DatabaseID:      "clone-db-id",
				DatabaseName:    "clone-db",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{
						Name:    "n1",
						HostIDs: []string{"host-1"},
						CloneConfig: &database.CloneConfig{
							SourceDatabaseID: "source-db-id",
							SourceNodeName:   "source-n1",
						},
					},
				},
			},
			expectedErr: "ZFS must be enabled for database cloning",
		},
		{
			name:   "valid clone config",
			cfg:    config.Config{ZFS: config.ZFS{Enabled: true, Pool: "tank"}},
			seedDB: true,
			spec: &database.Spec{
				DatabaseID:      "clone-db-id",
				DatabaseName:    "clone-db",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{
						Name:    "n1",
						HostIDs: []string{"host-1"},
						CloneConfig: &database.CloneConfig{
							SourceDatabaseID: "source-db-id",
							SourceNodeName:   "source-n1",
						},
					},
				},
			},
		},
		{
			name: "no clone config is always valid",
			cfg:  config.Config{},
			spec: &database.Spec{
				DatabaseID:      "clone-db-id",
				DatabaseName:    "clone-db",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					{
						Name:    "n1",
						HostIDs: []string{"host-1"},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := uuid.NewString()
			client := server.Client(t)
			store := database.NewStore(client, root)

			if tc.seedDB {
				seedSourceDatabase(t, root, "source-db-id", "source-n1", "host-1")
			}

			svc := database.NewService(tc.cfg, nil, store, nil, nil)
			err := svc.ValidateCloneConfig(t.Context(), tc.spec)

			if tc.expectedErr != "" {
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
