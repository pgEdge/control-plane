package swarm

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/docker"
)

func PostgresContainerExec(ctx context.Context, dockerClient *docker.Docker, instanceID uuid.UUID, cmd []string) (string, error) {
	container, err := GetPostgresContainer(ctx, dockerClient, instanceID)
	if err != nil {
		return "", fmt.Errorf("failed to get postgres container: %w", err)
	}
	output, err := dockerClient.Exec(ctx, container.ID, cmd)
	if err != nil {
		return "", fmt.Errorf("failed to exec command in postgres container: %w", err)
	}
	return output, nil
}
