package swarm

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
)

func PostgresContainerExec(ctx context.Context, w io.Writer, dockerClient *docker.Docker, instanceID uuid.UUID, cmd []string) error {
	container, err := GetPostgresContainer(ctx, dockerClient, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get postgres container: %w", err)
	}
	err = dockerClient.Exec(ctx, w, container.ID, cmd)
	if err != nil {
		return fmt.Errorf("failed to exec command in postgres container: %w", err)
	}
	return nil
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
