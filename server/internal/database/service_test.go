package database_test

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/stretchr/testify/assert"
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
		{
			name: "valid postgres major version change alongside a host reassignment",
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
				DatabaseName:    "test",
				PostgresVersion: "18.0",
				SpockVersion:    "5",
				Nodes: []*database.Node{
					// Unlike Spock, moving a node to a new host is the
					// supported way to bump its Postgres major version
					// during a rolling upgrade, so this is intentionally
					// exempt from the major-version check.
					{Name: "n1", HostIDs: []string{"host-2"}},
				},
			},
		},
		{
			name: "valid spock minor version change",
			current: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "test",
				PostgresVersion: "18.0",
				SpockVersion:    "5.0.6",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
				},
			},
			updated: &database.Spec{
				TenantID:        utils.PointerTo("tenant-id"),
				DatabaseName:    "test",
				PostgresVersion: "18.0",
				SpockVersion:    "5.0.9",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
				},
			},
		},
		{
			name: "invalid spock major version change",
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
				SpockVersion:    "6",
				Nodes: []*database.Node{
					{Name: "n1", HostIDs: []string{"host-1"}},
				},
			},
			expectedErr: "spock major version changed from 5 to 6",
		},
		{
			name: "invalid spock major version change alongside a host reassignment",
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
				SpockVersion:    "6",
				Nodes: []*database.Node{
					// Reassigning the node to a new host changes its
					// instance ID, so this must still be caught even though
					// no instance ID matches between current and updated.
					{Name: "n1", HostIDs: []string{"host-2"}},
				},
			},
			expectedErr: "spock major version changed from 5 to 6",
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
