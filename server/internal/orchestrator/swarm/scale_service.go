package swarm

import (
	"context"
	"fmt"
	"time"

	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
)

var _ resource.Resource = (*ScaleService)(nil)

const ResourceTypeScaleService resource.Type = "swarm.scale_service"

func ScaleServiceResourceIdentifier(instanceID string, direction ScaleDirection) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID + string(direction),
		Type: ResourceTypeScaleService,
	}
}

type ScaleDirection string

const (
	ScaleDirectionUP   ScaleDirection = "UP"
	ScaleDirectionDOWN ScaleDirection = "DOWN"
)

type ScaleService struct {
	InstanceID     string                `json:"instance_id"`
	ScaleDirection ScaleDirection        `json:"scale_direction"`
	Deps           []resource.Identifier `json:"deps"`
}

func (s *ScaleService) Executor() resource.Executor {
	return resource.ManagerExecutor()
}

func (s *ScaleService) Identifier() resource.Identifier {
	return ScaleServiceResourceIdentifier(s.InstanceID, s.ScaleDirection)
}

func (s *ScaleService) Dependencies() []resource.Identifier {
	return append([]resource.Identifier{PostgresServiceResourceIdentifier(s.InstanceID)}, s.Deps...)
}

func (s *ScaleService) Refresh(ctx context.Context, rc *resource.Context) error {
	return resource.ErrNotFound
}

func (s *ScaleService) Create(ctx context.Context, rc *resource.Context) error {
	var scaleDir uint64
	switch s.ScaleDirection {
	case ScaleDirectionUP:
		scaleDir = 1
	case ScaleDirectionDOWN:
		scaleDir = 0
	}

	dockerClient, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}
	svcResource, err := resource.FromContext[*PostgresService](rc, PostgresServiceResourceIdentifier(s.InstanceID))
	if err != nil {
		return fmt.Errorf("failed to get postgres service resource from state: %w", err)
	}

	err = dockerClient.ServiceScale(ctx, docker.ServiceScaleOptions{
		ServiceID:   svcResource.ServiceID,
		Scale:       scaleDir,
		Wait:        true,
		WaitTimeout: time.Minute,
	})
	if err != nil {
		return fmt.Errorf("failed to scale postgres service %s: %w", s.ScaleDirection, err)
	}
	return nil
}

func (s *ScaleService) Update(ctx context.Context, rc *resource.Context) error {
	return s.Create(ctx, rc)
}

func (s *ScaleService) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (s *ScaleService) DiffIgnore() []string {
	return nil
}

func (s *ScaleService) ResourceVersion() string {
	return "1"
}
