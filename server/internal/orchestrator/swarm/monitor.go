package swarm

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/docker"
)

func GetPostgresContainer(ctx context.Context, dockerClient *docker.Docker, instanceID uuid.UUID) (types.Container, error) {
	matches, err := dockerClient.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			// Multiple filters get AND'd together
			filters.Arg("label", fmt.Sprintf("pgedge.instance.id=%s", instanceID.String())),
			filters.Arg("label", fmt.Sprintf("pgedge.component=%s", "postgres")),
		),
	})
	if err != nil {
		return types.Container{}, fmt.Errorf("failed to list containers: %w", err)
	}
	if len(matches) == 0 {
		return types.Container{}, fmt.Errorf("no postgres container found for %q", instanceID.String())
	}
	return matches[0], nil
}
