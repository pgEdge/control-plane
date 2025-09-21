package swarm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var _ resource.Resource = (*CheckWillRestart)(nil)

const ResourceTypeCheckWillRestart resource.Type = "swarm.check_will_restart"

func CheckWillRestartIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypeCheckWillRestart,
	}
}

type CheckWillRestart struct {
	InstanceID  string `json:"instance_id"`
	CohortID    string `json:"cohort_id"`
	WillRestart bool   `json:"will_restart"`
}

func (s *CheckWillRestart) ResourceVersion() string {
	return "1"
}

func (s *CheckWillRestart) DiffIgnore() []string {
	return []string{"/will_restart"}
}

func (s *CheckWillRestart) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeCohort,
		ID:   s.CohortID,
	}
}

func (s *CheckWillRestart) Identifier() resource.Identifier {
	return CheckWillRestartIdentifier(s.InstanceID)
}

func (s *CheckWillRestart) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		PostgresServiceSpecResourceIdentifier(s.InstanceID),
	}
}

func (s *CheckWillRestart) Refresh(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (s *CheckWillRestart) Create(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	desired, err := resource.FromContext[*PostgresServiceSpecResource](rc, PostgresServiceSpecResourceIdentifier(s.InstanceID))
	if err != nil {
		return fmt.Errorf("failed to get desired service spec from state: %w", err)
	}

	current, err := client.ServiceInspectByLabels(ctx, map[string]string{
		"pgedge.component":   "postgres",
		"pgedge.instance.id": s.InstanceID,
	})
	if errors.Is(err, docker.ErrNotFound) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to get current service spec: %w", err)
	}

	// It's safe to modify the desired value. It's deserialized from JSON each
	// time it's retrieved from the state.
	desiredTask := s.normalizeTaskDefaults(desired.Spec.TaskTemplate)
	currentTask, err := s.normalizeCurrentTask(ctx, client, current.Spec.TaskTemplate)
	if err != nil {
		return err
	}

	s.WillRestart = !reflect.DeepEqual(currentTask, desiredTask)

	return nil
}

func (s *CheckWillRestart) Update(ctx context.Context, rc *resource.Context) error {
	return s.Create(ctx, rc)
}

func (s *CheckWillRestart) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (s *CheckWillRestart) normalizeTaskDefaults(spec swarm.TaskSpec) swarm.TaskSpec {
	if spec.ContainerSpec.StopGracePeriod == nil {
		spec.ContainerSpec.StopGracePeriod = utils.PointerTo(10 * time.Second)
	}
	if spec.ContainerSpec.Isolation.IsDefault() {
		spec.ContainerSpec.Isolation = container.IsolationDefault
	}
	if spec.ContainerSpec.DNSConfig == nil {
		spec.ContainerSpec.DNSConfig = &swarm.DNSConfig{}
	}
	if spec.Runtime == "" {
		spec.Runtime = swarm.RuntimeContainer
	}

	return spec
}

func (s *CheckWillRestart) normalizeCurrentTask(ctx context.Context, client *docker.Docker, current swarm.TaskSpec) (swarm.TaskSpec, error) {
	for i, n := range current.Networks {
		nw, err := client.NetworkInspect(ctx, n.Target, network.InspectOptions{})
		if err != nil {
			return swarm.TaskSpec{}, fmt.Errorf("failed to inspect current service network '%s': %w", n.Target, err)
		}
		normalized := swarm.NetworkAttachmentConfig{
			Target:     n.Target,
			Aliases:    n.Aliases,
			DriverOpts: n.DriverOpts,
		}
		if nw.Name == "bridge" {
			normalized.Target = "bridge"
		}

		current.Networks[i] = normalized
	}

	return s.normalizeTaskDefaults(current), nil
}
