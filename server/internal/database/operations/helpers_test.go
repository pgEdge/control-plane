package operations_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/database/operations"
	"github.com/pgEdge/control-plane/server/internal/monitor"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func asJSON(t testing.TB, expected, actual any) string {
	t.Helper()

	expectedRaw, err := json.MarshalIndent(expected, "  ", "  ")
	if err != nil {
		t.Fatal(err)
	}
	actualRaw, err := json.MarshalIndent(actual, "  ", "  ")
	if err != nil {
		t.Fatal(err)
	}

	return fmt.Sprintf("Expected:\n%s\nActual:\n%s\n", string(expectedRaw), string(actualRaw))
}

func makeMonitorResource(instance *database.InstanceResources) *monitor.InstanceMonitorResource {
	return &monitor.InstanceMonitorResource{
		DatabaseID:   instance.DatabaseID(),
		InstanceID:   instance.InstanceID(),
		DatabaseName: instance.DatabaseName(),
		HostID:       instance.HostID(),
	}
}

func makeState(t testing.TB, resources []resource.Resource, data []*resource.ResourceData) *resource.State {
	t.Helper()

	state := resource.NewState()
	for _, r := range resources {
		err := state.AddResource(r)
		if err != nil {
			t.Fatal(err)
		}
	}
	state.Add(data...)

	return state
}

func makeInstance(t testing.TB, node string, num int, dependencies ...resource.Resource) *database.InstanceResources {
	t.Helper()

	if len(dependencies) == 0 {
		dependencies = append(dependencies, makeOrchestratorResource(t, node, num, 1))
	}

	depIdentifiers := make([]resource.Identifier, len(dependencies))
	for i, dep := range dependencies {
		depIdentifiers[i] = dep.Identifier()
	}

	d, err := database.NewInstanceResources(
		&database.InstanceResource{
			Spec: &database.InstanceSpec{
				InstanceID:   fmt.Sprintf("%s-instance-%d-id", node, num),
				NodeName:     node,
				DatabaseID:   "database-id",
				HostID:       fmt.Sprintf("host-%d-id", num),
				DatabaseName: "test",
			},
			OrchestratorDependencies: depIdentifiers,
		},
		dependencies,
	)
	if err != nil {
		t.Fatal(err)
	}

	return d
}

func assertPlansEqual(t testing.TB, expected, actual []resource.PlanSummary) {
	t.Helper()

	require.Equal(t, len(expected), len(actual), "actual has different length:\n%s", asJSON(t, expected, actual))
	for i, plan := range actual {
		expectedPlan := expected[i]
		require.Equal(t, len(expectedPlan), len(plan), "actual[%d] has different length:\n%s", i, asJSON(t, expectedPlan, plan))

		for j, phase := range plan {
			expectedPhase := expectedPlan[j]
			// Operations within a phase are performed concurrently, so
			// the order of individual events is unimportant.
			assert.ElementsMatch(t, expectedPhase, phase, "actual[%d][%d] does not match:\n%s", i, j, asJSON(t, expectedPhase, phase))
		}
	}
}

var _ resource.Resource = (*orchestratorResource)(nil)

func makeOrchestratorResource(t testing.TB, node string, instanceNum, depNum int) *orchestratorResource {
	t.Helper()

	return &orchestratorResource{
		ID: fmt.Sprintf("%s-instance-%d-dep-%d-id", node, instanceNum, depNum),
	}
}

type orchestratorResource struct {
	ID string `json:"property"`
}

func (r *orchestratorResource) Executor() resource.Executor {
	return resource.AnyExecutor()
}

func (r *orchestratorResource) Identifier() resource.Identifier {
	return resource.Identifier{
		ID:   r.ID,
		Type: "orchestrator.resource",
	}
}

func (r *orchestratorResource) Dependencies() []resource.Identifier {
	return nil
}

func (r *orchestratorResource) Refresh(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *orchestratorResource) Create(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *orchestratorResource) Update(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *orchestratorResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (r *orchestratorResource) DiffIgnore() []string {
	return nil
}

func (r *orchestratorResource) ResourceVersion() string {
	return "1"
}

var _ resource.Resource = (*restoreResource)(nil)

func makeRestoreResource(t testing.TB, node string, instanceNum, depNum int) *restoreResource {
	t.Helper()

	return &restoreResource{
		orchestratorResource: orchestratorResource{
			ID: fmt.Sprintf("%s-instance-restore-%d-%d-id", node, instanceNum, depNum),
		},
	}
}

type restoreResource struct {
	orchestratorResource
}

func (r *restoreResource) Identifier() resource.Identifier {
	return resource.Identifier{
		ID:   r.ID,
		Type: "orchestrator.restore_resource",
	}
}

// Service resource stubs using the orchestratorResource embedding pattern.
// These mirror the real resource types' Identifier/Dependencies/DiffIgnore
// without importing the swarm package.

type serviceNetworkResource struct {
	orchestratorResource
	nodeNames []string
}

func (r *serviceNetworkResource) Identifier() resource.Identifier {
	return resource.Identifier{ID: r.ID, Type: "swarm.network"}
}

func (r *serviceNetworkResource) DiffIgnore() []string {
	return []string{"/network_id", "/subnet", "/gateway"}
}

func (r *serviceNetworkResource) Executor() resource.Executor {
	return resource.ManagerExecutor()
}

func (r *serviceNetworkResource) Dependencies() []resource.Identifier {
	var deps []resource.Identifier
	for _, name := range r.nodeNames {
		deps = append(deps, database.NodeResourceIdentifier(name))
	}
	return deps
}

type serviceUserRoleResource struct {
	orchestratorResource
	nodeNames []string
}

func (r *serviceUserRoleResource) Identifier() resource.Identifier {
	return resource.Identifier{ID: r.ID, Type: "swarm.service_user_role"}
}

func (r *serviceUserRoleResource) DiffIgnore() []string {
	return []string{"/postgres_host_id", "/username", "/password"}
}

func (r *serviceUserRoleResource) Dependencies() []resource.Identifier {
	var deps []resource.Identifier
	for _, name := range r.nodeNames {
		deps = append(deps, database.NodeResourceIdentifier(name))
	}
	return deps
}

type serviceInstanceSpecResource struct {
	orchestratorResource
	networkID         string
	serviceInstanceID string
}

func (r *serviceInstanceSpecResource) Identifier() resource.Identifier {
	return resource.Identifier{ID: r.ID, Type: "swarm.service_instance_spec"}
}

func (r *serviceInstanceSpecResource) DiffIgnore() []string {
	return []string{"/spec"}
}

func (r *serviceInstanceSpecResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		{ID: r.networkID, Type: "swarm.network"},
		{ID: r.serviceInstanceID, Type: "swarm.service_user_role"},
	}
}

type serviceInstanceResource struct {
	orchestratorResource
	serviceInstanceID string
}

func (r *serviceInstanceResource) Identifier() resource.Identifier {
	return resource.Identifier{ID: r.ID, Type: "swarm.service_instance"}
}

func (r *serviceInstanceResource) DiffIgnore() []string {
	return []string{"/database_id", "/service_id", "/host_id"}
}

func (r *serviceInstanceResource) Executor() resource.Executor {
	return resource.ManagerExecutor()
}

func (r *serviceInstanceResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		{ID: r.serviceInstanceID, Type: "swarm.service_user_role"},
		{ID: r.serviceInstanceID, Type: "swarm.service_instance_spec"},
	}
}

func makeServiceResources(t testing.TB, databaseID, serviceID, hostID string, nodeNames []string) *operations.ServiceResources {
	t.Helper()

	serviceInstanceID := database.GenerateServiceInstanceID(databaseID, serviceID, hostID)
	databaseNetworkID := database.GenerateDatabaseNetworkID(databaseID)

	resources := []resource.Resource{
		&serviceNetworkResource{
			orchestratorResource: orchestratorResource{ID: databaseNetworkID},
			nodeNames:            nodeNames,
		},
		&serviceUserRoleResource{
			orchestratorResource: orchestratorResource{ID: serviceInstanceID},
			nodeNames:            nodeNames,
		},
		&serviceInstanceSpecResource{
			orchestratorResource: orchestratorResource{ID: serviceInstanceID},
			networkID:            databaseNetworkID,
			serviceInstanceID:    serviceInstanceID,
		},
		&serviceInstanceResource{
			orchestratorResource: orchestratorResource{ID: serviceInstanceID},
			serviceInstanceID:    serviceInstanceID,
		},
	}

	resourceData := make([]*resource.ResourceData, len(resources))
	for i, res := range resources {
		rd, err := resource.ToResourceData(res)
		if err != nil {
			t.Fatal(err)
		}
		resourceData[i] = rd
	}

	monitorResource := &monitor.ServiceInstanceMonitorResource{
		DatabaseID:        databaseID,
		ServiceInstanceID: serviceInstanceID,
		HostID:            hostID,
	}

	return &operations.ServiceResources{
		ServiceInstanceID: serviceInstanceID,
		Resources:         resourceData,
		MonitorResource:   monitorResource,
	}
}
