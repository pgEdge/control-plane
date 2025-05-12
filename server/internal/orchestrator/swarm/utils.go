package swarm

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/ds"
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

func PgBackRestBackupCmd(command string, args ...string) pgbackrest.Cmd {
	return pgbackrest.Cmd{
		PgBackrestCmd: "/usr/bin/pgbackrest",
		Config:        "/opt/pgedge/configs/pgbackrest.backup.conf",
		Stanza:        "db",
		Command:       command,
		Args:          args,
	}
}

var targetActionRestoreTypes = ds.NewSet(
	"immediate",
	"lsn",
	"name",
	"time",
	"xid",
)

func PgBackRestRestoreCmd(command string, args ...string) pgbackrest.Cmd {
	var hasTargetAction, needsTargetAction bool
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--target-action") {
			hasTargetAction = true
			continue // skip the next arg since it's the value of --target-action no further checks needed
		}
		var restoreType string
		if arg == "--type" && i+1 < len(args) {
			restoreType = args[i+1]
			i++ // skip the next arg since it's the value of --type
		} else if strings.HasPrefix(arg, "--type=") {
			restoreType = strings.TrimPrefix(arg, "--type=")
		} else {
			continue
		}
		if targetActionRestoreTypes.Has(restoreType) {
			needsTargetAction = true
		}
	}
	if needsTargetAction && !hasTargetAction {
		args = append(args, "--target-action=promote")
	}

	return pgbackrest.Cmd{
		PgBackrestCmd: "/usr/bin/pgbackrest",
		Config:        "/opt/pgedge/configs/pgbackrest.restore.conf",
		Stanza:        "db",
		Command:       command,
		Args:          args,
	}
}
