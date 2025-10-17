package operations_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
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

func makeResourceData(t testing.TB, r resource.Resource) *resource.ResourceData {
	t.Helper()

	d, err := resource.ToResourceData(r)
	if err != nil {
		t.Fatal(err)
	}

	return d
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

func assertPlansEqual(t testing.TB, expected, actual []resource.Plan) {
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
