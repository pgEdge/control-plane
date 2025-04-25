package swarm

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
)

func PostgresContainerExec(ctx context.Context, dockerClient *docker.Docker, instanceID uuid.UUID, cmd []string) ([]byte, error) {
	container, err := GetPostgresContainer(ctx, dockerClient, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get postgres container: %w", err)
	}
	output, err := dockerClient.Exec(ctx, container.ID, cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to exec command in postgres container: %w", err)
	}
	return output, nil
}

func pgbackrestBackupCmd(command string, args ...string) pgbackrest.Cmd {
	return pgbackrest.Cmd{
		PgBackrestCmd: "/usr/bin/pgbackrest",
		Config:        "/opt/pgedge/configs/pgbackrest.backup.conf",
		Stanza:        "db",
		Command:       command,
		Args:          args,
	}
}
