package swarm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var _ resource.Resource = (*PostgresService)(nil)

const ResourceTypePostgresService resource.Type = "swarm.postgres_service"

func PostgresServiceResourceIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypePostgresService,
	}
}

type PostgresService struct {
	InstanceID  string `json:"instance_id"`
	ServiceName string `json:"service_name"`
	ServiceID   string `json:"service_id"`
	NeedsUpdate bool   `json:"needs_update"`
}

func (s *PostgresService) ResourceVersion() string {
	return "1"
}

func (s *PostgresService) DiffIgnore() []string {
	return []string{
		"/service_id",
	}
}

func (s *PostgresService) Identifier() resource.Identifier {
	return PostgresServiceResourceIdentifier(s.InstanceID)
}

func (s *PostgresService) Executor() resource.Executor {
	return resource.ManagerExecutor()
}

func (s *PostgresService) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		PostgresServiceSpecResourceIdentifier(s.InstanceID),
		SwitchoverResourceIdentifier(s.InstanceID),
		CheckWillRestartIdentifier(s.InstanceID),
	}
}

func (s *PostgresService) Refresh(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	desired, err := resource.FromContext[*PostgresServiceSpecResource](rc, PostgresServiceSpecResourceIdentifier(s.InstanceID))
	if err != nil {
		return fmt.Errorf("failed to get desired service spec from state: %w", err)
	}
	// This CheckWillRestart resource already does a normalized task diff to
	// determine if an update would cause a restart. We can reuse the output of
	// that check in our needsUpdate helper.
	willRestart, err := resource.FromContext[*CheckWillRestart](rc, CheckWillRestartIdentifier(s.InstanceID))
	if err != nil {
		return fmt.Errorf("failed to get 'check will restart' from state: %w", err)
	}

	resp, err := client.ServiceInspectByLabels(ctx, map[string]string{
		"pgedge.component":   "postgres",
		"pgedge.instance.id": s.InstanceID,
	})
	if errors.Is(err, docker.ErrNotFound) {
		return resource.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("failed to inspect postgres service: %w", err)
	}
	s.ServiceID = resp.ID
	s.ServiceName = resp.Spec.Name
	s.NeedsUpdate = s.needsUpdate(resp.Spec, desired.Spec, willRestart.WillRestart)

	return nil
}

func (s *PostgresService) Create(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	specResourceID := PostgresServiceSpecResourceIdentifier(s.InstanceID)
	spec, err := resource.FromContext[*PostgresServiceSpecResource](rc, specResourceID)
	if err != nil {
		return fmt.Errorf("failed to get postgres service spec from state: %w", err)
	}

	res, err := client.ServiceDeploy(ctx, spec.Spec)
	if err != nil {
		return fmt.Errorf("failed to deploy postgres service: %w", err)
	}

	s.ServiceID = res.ServiceID

	if err := client.WaitForService(ctx, res.ServiceID, 5*time.Minute, res.Previous); err != nil {
		return fmt.Errorf("failed to wait for postgres service to start: %w", err)
	}

	return nil
}

func (s *PostgresService) Update(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	resp, err := client.ServiceInspectByLabels(ctx, map[string]string{
		"pgedge.component":   "postgres",
		"pgedge.instance.id": s.InstanceID,
	})
	if err != nil && !errors.Is(err, docker.ErrNotFound) {
		return fmt.Errorf("failed to inspect postgres service: %w", err)
	}
	if err == nil && resp.Spec.Name != s.ServiceName {
		// If the service name has changed, we need to remove the service with
		// the old name so that it can be recreated with the new name.
		if err := client.ServiceRemove(ctx, resp.Spec.Name); err != nil {
			return fmt.Errorf("failed to remove postgres service for service name update: %w", err)
		}
	}

	return s.Create(ctx, rc)
}

func (s *PostgresService) Delete(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	// We scale down before removing the service so that we can guarantee that
	// the containers have stopped before this function returns. Otherwise, we
	// can encounter errors from trying to remove other resources while the
	// containers are still up.
	err = client.ServiceScale(ctx, docker.ServiceScaleOptions{
		ServiceID:   s.ServiceName,
		Scale:       0,
		Wait:        true,
		WaitTimeout: time.Minute,
	})
	switch {
	case errors.Is(err, docker.ErrNotFound):
		// Service is already deleted.
		return nil
	case errors.Is(err, docker.ErrNodeUnavailable):
		// The node running this service is down. This is expected during
		// remove-host --force operations. Proceed with service removal since
		// the container cannot be gracefully stopped.
	case err != nil:
		return fmt.Errorf("failed to scale down postgres service before removal: %w", err)
	}

	if err := client.ServiceRemove(ctx, s.ServiceName); err != nil {
		return fmt.Errorf("failed to remove postgres service: %w", err)
	}

	return nil
}

func (s *PostgresService) needsUpdate(curr swarm.ServiceSpec, desired swarm.ServiceSpec, willRestart bool) bool {
	if curr.Mode.Replicated != nil && utils.FromPointer(curr.Mode.Replicated.Replicas) != 1 {
		// This means that the service is scaled down
		return true
	}

	if willRestart {
		// This means that the task definition differs between the current and
		// desired specs.
		return true
	}

	return !reflect.DeepEqual(curr.EndpointSpec, desired.EndpointSpec) || !reflect.DeepEqual(curr.Annotations, desired.Annotations)
}
