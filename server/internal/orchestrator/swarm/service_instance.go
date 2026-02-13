package swarm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var _ resource.Resource = (*ServiceInstanceResource)(nil)

const ResourceTypeServiceInstance resource.Type = database.ResourceTypeServiceInstance

func ServiceInstanceResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeServiceInstance,
	}
}

type ServiceInstanceResource struct {
	ServiceInstanceID string `json:"service_instance_id"`
	DatabaseID        string `json:"database_id"`
	ServiceName       string `json:"service_name"`
	ServiceID         string `json:"service_id"`
	NeedsUpdate       bool   `json:"needs_update"`
}

func (s *ServiceInstanceResource) ResourceVersion() string {
	return "1"
}

func (s *ServiceInstanceResource) DiffIgnore() []string {
	return []string{
		"/database_id",
		"/service_id",
	}
}

func (s *ServiceInstanceResource) Identifier() resource.Identifier {
	return ServiceInstanceResourceIdentifier(s.ServiceInstanceID)
}

func (s *ServiceInstanceResource) Executor() resource.Executor {
	return resource.ManagerExecutor()
}

func (s *ServiceInstanceResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		ServiceUserRoleIdentifier(s.ServiceInstanceID),
		ServiceInstanceSpecResourceIdentifier(s.ServiceInstanceID),
	}
}

func (s *ServiceInstanceResource) Refresh(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	desired, err := resource.FromContext[*ServiceInstanceSpecResource](rc, ServiceInstanceSpecResourceIdentifier(s.ServiceInstanceID))
	if err != nil {
		return fmt.Errorf("failed to get desired service spec from state: %w", err)
	}

	resp, err := client.ServiceInspectByLabels(ctx, map[string]string{
		"pgedge.component":           "service",
		"pgedge.service.instance.id": s.ServiceInstanceID,
	})
	if errors.Is(err, docker.ErrNotFound) {
		return resource.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("failed to inspect service instance: %w", err)
	}
	s.ServiceID = resp.ID
	s.ServiceName = resp.Spec.Name
	s.NeedsUpdate = s.needsUpdate(resp.Spec, desired.Spec)

	return nil
}

func (s *ServiceInstanceResource) Create(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}

	specResourceID := ServiceInstanceSpecResourceIdentifier(s.ServiceInstanceID)
	spec, err := resource.FromContext[*ServiceInstanceSpecResource](rc, specResourceID)
	if err != nil {
		return fmt.Errorf("failed to get service instance spec from state: %w", err)
	}

	logger.Info().
		Str("service_instance_id", s.ServiceInstanceID).
		Str("image", spec.Spec.TaskTemplate.ContainerSpec.Image).
		Msg("deploying service instance")

	res, err := client.ServiceDeploy(ctx, spec.Spec)
	if err != nil {
		return fmt.Errorf("failed to deploy service instance: %w", err)
	}

	s.ServiceID = res.ServiceID

	logger.Info().
		Str("service_instance_id", s.ServiceInstanceID).
		Str("service_id", res.ServiceID).
		Msg("service deployed, waiting for tasks to start")

	if err := client.WaitForService(ctx, res.ServiceID, 5*time.Minute, res.Previous); err != nil {
		logger.Error().
			Err(err).
			Str("service_instance_id", s.ServiceInstanceID).
			Str("service_id", res.ServiceID).
			Msg("service instance failed to start")
		return fmt.Errorf("failed to wait for service instance to start: %w", err)
	}

	logger.Info().
		Str("service_instance_id", s.ServiceInstanceID).
		Str("service_id", res.ServiceID).
		Msg("service instance started successfully")

	// Transition state to "running" immediately after successful deployment.
	// This mirrors the database instance pattern where state is set
	// deterministically within the resource activity, not deferred to the monitor.
	dbSvc, err := do.Invoke[*database.Service](rc.Injector)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get database service for state update")
	} else {
		if err := dbSvc.SetServiceInstanceState(ctx, s.DatabaseID, s.ServiceInstanceID, database.ServiceInstanceStateRunning); err != nil {
			logger.Warn().Err(err).
				Str("service_instance_id", s.ServiceInstanceID).
				Msg("failed to update service instance state to running (monitor will handle it)")
		}
	}

	return nil
}

func (s *ServiceInstanceResource) Update(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	resp, err := client.ServiceInspectByLabels(ctx, map[string]string{
		"pgedge.component":           "service",
		"pgedge.service.instance.id": s.ServiceInstanceID,
	})
	if err != nil && !errors.Is(err, docker.ErrNotFound) {
		return fmt.Errorf("failed to inspect service instance: %w", err)
	}
	if err == nil && resp.Spec.Name != s.ServiceName {
		// If the service name has changed, we need to remove the service with
		// the old name so that it can be recreated with the new name.
		if err := client.ServiceRemove(ctx, resp.Spec.Name); err != nil {
			return fmt.Errorf("failed to remove service instance for service name update: %w", err)
		}
	}

	return s.Create(ctx, rc)
}

func (s *ServiceInstanceResource) Delete(ctx context.Context, rc *resource.Context) error {
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
	if errors.Is(err, docker.ErrNotFound) {
		// Service is already deleted, but still try to clean up etcd state
	} else if err != nil {
		return fmt.Errorf("failed to scale down service instance before removal: %w", err)
	} else {
		// Only try to remove the service if it exists
		if err := client.ServiceRemove(ctx, s.ServiceName); err != nil {
			return fmt.Errorf("failed to remove service instance: %w", err)
		}
	}

	// Remove service instance from etcd storage
	svc, err := do.Invoke[*database.Service](rc.Injector)
	if err != nil {
		return err
	}

	err = svc.DeleteServiceInstance(ctx, s.DatabaseID, s.ServiceInstanceID)
	if err != nil {
		return err
	}

	return nil
}

func (s *ServiceInstanceResource) needsUpdate(curr swarm.ServiceSpec, desired swarm.ServiceSpec) bool {
	if curr.Mode.Replicated != nil && utils.FromPointer(curr.Mode.Replicated.Replicas) != 1 {
		// This means that the service is scaled down
		return true
	}

	// For service instances, we do a simple comparison of the task spec
	// We don't have a CheckWillRestart resource like PostgresService does
	return !reflect.DeepEqual(curr.TaskTemplate, desired.TaskTemplate) ||
		!reflect.DeepEqual(curr.EndpointSpec, desired.EndpointSpec) ||
		!reflect.DeepEqual(curr.Annotations, desired.Annotations)
}
