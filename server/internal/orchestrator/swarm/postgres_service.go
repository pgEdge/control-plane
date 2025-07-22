package swarm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
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
	Instance    *database.InstanceSpec `json:"instance"`
	CohortID    string                 `json:"cohort_id"`
	ServiceName string                 `json:"service_name"`
	ServiceID   string                 `json:"service_id"`
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
	return PostgresServiceResourceIdentifier(s.Instance.InstanceID)
}

func (s *PostgresService) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeCohort,
		ID:   s.CohortID,
	}
}

func (s *PostgresService) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		PostgresServiceSpecResourceIdentifier(s.Instance.InstanceID),
	}
}

func (s *PostgresService) Refresh(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	resp, err := client.ServiceInspectByLabels(ctx, map[string]string{
		"pgedge.component":   "postgres",
		"pgedge.instance.id": s.Instance.InstanceID,
	})
	if errors.Is(err, docker.ErrNotFound) {
		return resource.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("failed to inspect postgres service: %w", err)
	}
	s.ServiceID = resp.ID
	s.ServiceName = resp.Spec.Name

	return nil
}

func (s *PostgresService) Create(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	specResourceID := PostgresServiceSpecResourceIdentifier(s.Instance.InstanceID)
	spec, err := resource.FromContext[*PostgresServiceSpecResource](rc, specResourceID)
	if err != nil {
		return fmt.Errorf("failed to get postgres service spec from state: %w", err)
	}

	serviceID, err := client.ServiceDeploy(ctx, spec.Spec)
	if err != nil {
		return fmt.Errorf("failed to deploy postgres service: %w", err)
	}

	s.ServiceID = serviceID

	// TODO: this might need to be a lot longer if we use Patroni's bootstrap
	// functionality to restore from backup or from another node.
	if err := client.WaitForService(ctx, serviceID, 5*time.Minute); err != nil {
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
		"pgedge.instance.id": s.Instance.InstanceID,
	})
	if err == nil && resp.Spec.Name != s.ServiceName {
		// If the service name has changed, we need to remove the service with
		// the old name so that it can be recreated with the new name.
		if err := client.ServiceRemove(ctx, resp.Spec.Name); err != nil {
			return fmt.Errorf("failed to remove postgres service for service name update: %w", err)
		}
	} else if !errors.Is(err, docker.ErrNotFound) {
		return fmt.Errorf("failed to inspect postgres service: %w", err)
	}

	return s.Create(ctx, rc)
}

func (s *PostgresService) Delete(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	if err := client.ServiceRemove(ctx, s.ServiceName); err != nil {
		return fmt.Errorf("failed to remove postgres service: %w", err)
	}

	return nil
}
