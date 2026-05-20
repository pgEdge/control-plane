package database_test

import (
	"testing"
	"time"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/stretchr/testify/assert"
)

func TestReconcileVersions(t *testing.T) {
	for _, tc := range []struct {
		name              string
		spec              *database.StoredSpec
		instances         []*database.StoredInstance
		statuses          []*database.StoredInstanceStatus
		expectedSpec      *database.StoredSpec
		expectedInstances []*database.StoredInstance
	}{
		{
			name: "observed matches spec",
			spec: &database.StoredSpec{
				Spec: &database.Spec{
					PostgresVersion: "17.4",
					SpockVersion:    "5",
					Nodes: []*database.Node{
						{Name: "n1", HostIDs: []string{"host-1"}},
					},
				},
			},
			instances: []*database.StoredInstance{
				{
					InstanceID:    "n1-host-1",
					NodeName:      "n1",
					HostID:        "host-1",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.4", "5"),
				},
			},
			statuses: []*database.StoredInstanceStatus{
				{
					InstanceID: "n1-host-1",
					Status: &database.InstanceStatus{
						StatusUpdatedAt: utils.PointerTo(time.Now()),
						Role:            utils.PointerTo(patroni.InstanceRolePrimary),
						PostgresVersion: utils.PointerTo("17.4"),
						SpockVersion:    utils.PointerTo("5.0.6"),
					},
				},
			},
		},
		{
			name: "all nodes updated",
			spec: &database.StoredSpec{
				Spec: &database.Spec{
					PostgresVersion: "17.4",
					SpockVersion:    "5",
					Nodes: []*database.Node{
						{Name: "n1", HostIDs: []string{"host-1"}},
						{Name: "n2", HostIDs: []string{"host-2"}},
					},
				},
			},
			instances: []*database.StoredInstance{
				{
					InstanceID:    "n1-host-1",
					NodeName:      "n1",
					HostID:        "host-1",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.4", "5"),
				},
				{
					InstanceID:    "n2-host-2",
					NodeName:      "n2",
					HostID:        "host-2",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.4", "5"),
				},
			},
			statuses: []*database.StoredInstanceStatus{
				{
					InstanceID: "n1-host-1",
					Status: &database.InstanceStatus{
						StatusUpdatedAt: utils.PointerTo(time.Now()),
						Role:            utils.PointerTo(patroni.InstanceRolePrimary),
						PostgresVersion: utils.PointerTo("17.5"),
						SpockVersion:    utils.PointerTo("6.0.0"),
					},
				},
				{
					InstanceID: "n2-host-2",
					Status: &database.InstanceStatus{
						StatusUpdatedAt: utils.PointerTo(time.Now()),
						Role:            utils.PointerTo(patroni.InstanceRolePrimary),
						PostgresVersion: utils.PointerTo("17.5"),
						SpockVersion:    utils.PointerTo("6.0.0"),
					},
				},
			},
			expectedSpec: &database.StoredSpec{
				Spec: &database.Spec{
					PostgresVersion: "17.5",
					SpockVersion:    "6",
					Nodes: []*database.Node{
						{Name: "n1", HostIDs: []string{"host-1"}},
						{Name: "n2", HostIDs: []string{"host-2"}},
					},
				},
			},
			expectedInstances: []*database.StoredInstance{
				{
					InstanceID:    "n1-host-1",
					NodeName:      "n1",
					HostID:        "host-1",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.5", "6"),
				},
				{
					InstanceID:    "n2-host-2",
					NodeName:      "n2",
					HostID:        "host-2",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.5", "6"),
				},
			},
		},
		{
			name: "all nodes updated spock only",
			spec: &database.StoredSpec{
				Spec: &database.Spec{
					PostgresVersion: "17.4",
					SpockVersion:    "5",
					Nodes: []*database.Node{
						{
							Name:            "n1",
							HostIDs:         []string{"host-1"},
							PostgresVersion: "17.5",
						},
						{
							Name:            "n2",
							HostIDs:         []string{"host-2"},
							PostgresVersion: "17.5",
						},
					},
				},
			},
			instances: []*database.StoredInstance{
				{
					InstanceID:    "n1-host-1",
					NodeName:      "n1",
					HostID:        "host-1",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.5", "5"),
				},
				{
					InstanceID:    "n2-host-2",
					NodeName:      "n2",
					HostID:        "host-2",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.5", "5"),
				},
			},
			statuses: []*database.StoredInstanceStatus{
				{
					InstanceID: "n1-host-1",
					Status: &database.InstanceStatus{
						StatusUpdatedAt: utils.PointerTo(time.Now()),
						Role:            utils.PointerTo(patroni.InstanceRolePrimary),
						PostgresVersion: utils.PointerTo("17.5"),
						SpockVersion:    utils.PointerTo("6.0.0"),
					},
				},
				{
					InstanceID: "n2-host-2",
					Status: &database.InstanceStatus{
						StatusUpdatedAt: utils.PointerTo(time.Now()),
						Role:            utils.PointerTo(patroni.InstanceRolePrimary),
						PostgresVersion: utils.PointerTo("17.5"),
						SpockVersion:    utils.PointerTo("6.0.0"),
					},
				},
			},
			expectedSpec: &database.StoredSpec{
				Spec: &database.Spec{
					PostgresVersion: "17.4",
					SpockVersion:    "6",
					Nodes: []*database.Node{
						{
							Name:            "n1",
							HostIDs:         []string{"host-1"},
							PostgresVersion: "17.5", // These overrides should remain unnormalized since only the spock version changed
						},
						{
							Name:            "n2",
							HostIDs:         []string{"host-2"},
							PostgresVersion: "17.5",
						},
					},
				},
			},
			expectedInstances: []*database.StoredInstance{
				{
					InstanceID:    "n1-host-1",
					NodeName:      "n1",
					HostID:        "host-1",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.5", "6"),
				},
				{
					InstanceID:    "n2-host-2",
					NodeName:      "n2",
					HostID:        "host-2",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.5", "6"),
				},
			},
		},
		{
			name: "all nodes updated with override",
			spec: &database.StoredSpec{
				Spec: &database.Spec{
					PostgresVersion: "17.4",
					SpockVersion:    "5",
					Nodes: []*database.Node{
						{Name: "n1", HostIDs: []string{"host-1"}, PostgresVersion: "17.5"},
						{Name: "n2", HostIDs: []string{"host-2"}},
					},
				},
			},
			instances: []*database.StoredInstance{
				{
					InstanceID:    "n1-host-1",
					NodeName:      "n1",
					HostID:        "host-1",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.4", "5"),
				},
				{
					InstanceID:    "n2-host-2",
					NodeName:      "n2",
					HostID:        "host-2",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.4", "5"),
				},
			},
			statuses: []*database.StoredInstanceStatus{
				{
					InstanceID: "n1-host-1",
					Status: &database.InstanceStatus{
						StatusUpdatedAt: utils.PointerTo(time.Now()),
						Role:            utils.PointerTo(patroni.InstanceRolePrimary),
						PostgresVersion: utils.PointerTo("17.5"),
						SpockVersion:    utils.PointerTo("6.0.0"),
					},
				},
				{
					InstanceID: "n2-host-2",
					Status: &database.InstanceStatus{
						StatusUpdatedAt: utils.PointerTo(time.Now()),
						Role:            utils.PointerTo(patroni.InstanceRolePrimary),
						PostgresVersion: utils.PointerTo("17.5"),
						SpockVersion:    utils.PointerTo("6.0.0"),
					},
				},
			},
			expectedSpec: &database.StoredSpec{
				Spec: &database.Spec{
					PostgresVersion: "17.5",
					SpockVersion:    "6",
					Nodes: []*database.Node{
						{Name: "n1", HostIDs: []string{"host-1"}},
						{Name: "n2", HostIDs: []string{"host-2"}},
					},
				},
			},
			expectedInstances: []*database.StoredInstance{
				{
					InstanceID:    "n1-host-1",
					NodeName:      "n1",
					HostID:        "host-1",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.5", "6"),
				},
				{
					InstanceID:    "n2-host-2",
					NodeName:      "n2",
					HostID:        "host-2",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.5", "6"),
				},
			},
		},
		{
			name: "one node updated",
			spec: &database.StoredSpec{
				Spec: &database.Spec{
					PostgresVersion: "17.4",
					SpockVersion:    "5",
					Nodes: []*database.Node{
						{Name: "n1", HostIDs: []string{"host-1"}},
						{Name: "n2", HostIDs: []string{"host-2"}},
					},
				},
			},
			instances: []*database.StoredInstance{
				{
					InstanceID:    "n1-host-1",
					NodeName:      "n1",
					HostID:        "host-1",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.4", "5"),
				},
				{
					InstanceID:    "n2-host-2",
					NodeName:      "n2",
					HostID:        "host-2",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.4", "5"),
				},
			},
			statuses: []*database.StoredInstanceStatus{
				{
					InstanceID: "n1-host-1",
					Status: &database.InstanceStatus{
						StatusUpdatedAt: utils.PointerTo(time.Now()),
						Role:            utils.PointerTo(patroni.InstanceRolePrimary),
						PostgresVersion: utils.PointerTo("17.4"),
						SpockVersion:    utils.PointerTo("5.0.6"),
					},
				},
				{
					InstanceID: "n2-host-2",
					Status: &database.InstanceStatus{
						StatusUpdatedAt: utils.PointerTo(time.Now()),
						Role:            utils.PointerTo(patroni.InstanceRolePrimary),
						PostgresVersion: utils.PointerTo("17.5"),
						SpockVersion:    utils.PointerTo("6.0.0"),
					},
				},
			},
			expectedSpec: &database.StoredSpec{
				Spec: &database.Spec{
					PostgresVersion: "17.4",
					SpockVersion:    "5",
					Nodes: []*database.Node{
						{Name: "n1", HostIDs: []string{"host-1"}},
						{Name: "n2", HostIDs: []string{"host-2"}, PostgresVersion: "17.5"},
					},
				},
			},
			expectedInstances: []*database.StoredInstance{
				{
					InstanceID:    "n2-host-2",
					NodeName:      "n2",
					HostID:        "host-2",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.5", "6"),
				},
			},
		},
		{
			name: "one node updated, one with stale status",
			spec: &database.StoredSpec{
				Spec: &database.Spec{
					PostgresVersion: "17.4",
					SpockVersion:    "5",
					Nodes: []*database.Node{
						{Name: "n1", HostIDs: []string{"host-1"}},
						{Name: "n2", HostIDs: []string{"host-2"}},
					},
				},
			},
			instances: []*database.StoredInstance{
				{
					InstanceID:    "n1-host-1",
					NodeName:      "n1",
					HostID:        "host-1",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.4", "5"),
				},
				{
					InstanceID:    "n2-host-2",
					NodeName:      "n2",
					HostID:        "host-2",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.4", "5"),
				},
			},
			statuses: []*database.StoredInstanceStatus{
				{
					InstanceID: "n1-host-1",
					Status: &database.InstanceStatus{
						StatusUpdatedAt: utils.PointerTo(time.Now().Add(-3 * database.InstanceMonitorRefreshInterval)),
						Role:            utils.PointerTo(patroni.InstanceRolePrimary),
						PostgresVersion: utils.PointerTo("17.5"),
						SpockVersion:    utils.PointerTo("6.0.0"),
					},
				},
				{
					InstanceID: "n2-host-2",
					Status: &database.InstanceStatus{
						StatusUpdatedAt: utils.PointerTo(time.Now()),
						Role:            utils.PointerTo(patroni.InstanceRolePrimary),
						PostgresVersion: utils.PointerTo("17.5"),
						SpockVersion:    utils.PointerTo("6.0.0"),
					},
				},
			},
			expectedSpec: &database.StoredSpec{
				Spec: &database.Spec{
					PostgresVersion: "17.4",
					SpockVersion:    "5",
					Nodes: []*database.Node{
						{Name: "n1", HostIDs: []string{"host-1"}},
						{Name: "n2", HostIDs: []string{"host-2"}, PostgresVersion: "17.5"},
					},
				},
			},
			expectedInstances: []*database.StoredInstance{
				{
					InstanceID:    "n2-host-2",
					NodeName:      "n2",
					HostID:        "host-2",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.5", "6"),
				},
			},
		},
		{
			name: "not all instances updated",
			spec: &database.StoredSpec{
				Spec: &database.Spec{
					PostgresVersion: "17.4",
					SpockVersion:    "5",
					Nodes: []*database.Node{
						{Name: "n1", HostIDs: []string{"host-1", "host-2"}},
					},
				},
			},
			instances: []*database.StoredInstance{
				{
					InstanceID:    "n1-host-1",
					NodeName:      "n1",
					HostID:        "host-1",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.4", "5"),
				},
				{
					InstanceID:    "n1-host-2",
					NodeName:      "n1",
					HostID:        "host-2",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.4", "5"),
				},
			},
			statuses: []*database.StoredInstanceStatus{
				{
					InstanceID: "n1-host-1",
					Status: &database.InstanceStatus{
						StatusUpdatedAt: utils.PointerTo(time.Now()),
						Role:            utils.PointerTo(patroni.InstanceRoleReplica),
						PostgresVersion: utils.PointerTo("17.4"),
						SpockVersion:    utils.PointerTo("5.0.6"),
					},
				},
				{
					InstanceID: "n1-host-2",
					Status: &database.InstanceStatus{
						StatusUpdatedAt: utils.PointerTo(time.Now()),
						Role:            utils.PointerTo(patroni.InstanceRoleReplica),
						PostgresVersion: utils.PointerTo("17.5"),
						SpockVersion:    utils.PointerTo("6.0.0"),
					},
				},
			},
			expectedInstances: []*database.StoredInstance{
				{
					InstanceID:    "n1-host-2",
					NodeName:      "n1",
					HostID:        "host-2",
					PgEdgeVersion: ds.MustParsePgEdgeVersion("17.5", "6"),
				},
			},
		},
		{
			name: "no instances",
			spec: &database.StoredSpec{
				Spec: &database.Spec{
					PostgresVersion: "17.4",
					SpockVersion:    "5",
					Nodes: []*database.Node{
						{Name: "n1"},
					},
				},
			},
		},
		{
			name: "malformed instance records",
			spec: &database.StoredSpec{
				Spec: &database.Spec{
					PostgresVersion: "17.4",
					SpockVersion:    "5",
					Nodes: []*database.Node{
						{Name: "n1", HostIDs: []string{"host-1"}},
						{Name: "n2", HostIDs: []string{"host-2"}},
					},
				},
			},
			// These instances are missing a PgEdgeVersion due to a failure
			// somewhere else in the system.
			instances: []*database.StoredInstance{
				{
					InstanceID: "n1-host-1",
					NodeName:   "n1",
					HostID:     "host-1",
				},
				{
					InstanceID: "n2-host-2",
					NodeName:   "n2",
					HostID:     "host-2",
				},
			},
			// These instances are up and running even though the instance
			// records are malformed. Otherwise, reconcileVersions will skip
			// these instances.
			statuses: []*database.StoredInstanceStatus{
				{
					InstanceID: "n1-host-1",
					Status: &database.InstanceStatus{
						StatusUpdatedAt: utils.PointerTo(time.Now()),
						Role:            utils.PointerTo(patroni.InstanceRoleReplica),
						PostgresVersion: utils.PointerTo("17.4"),
						SpockVersion:    utils.PointerTo("5.0.6"),
					},
				},
				{
					InstanceID: "n2-host-2",
					Status: &database.InstanceStatus{
						StatusUpdatedAt: utils.PointerTo(time.Now()),
						Role:            utils.PointerTo(patroni.InstanceRoleReplica),
						PostgresVersion: utils.PointerTo("17.4"),
						SpockVersion:    utils.PointerTo("5.0.6"),
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updatedSpec, updatedInstances := database.ReconcileVersions(
				tc.spec,
				tc.instances,
				tc.statuses,
			)
			assert.Equal(t, tc.expectedSpec, updatedSpec)
			assert.Equal(t, tc.expectedInstances, updatedInstances)
		})
	}
}
