package swarm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"

	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
)

var (
	ErrNoPostgresContainer = errors.New("no postgres container found")
	ErrNoPostgresService   = errors.New("no postgres service found")
)

func GetPostgresContainer(ctx context.Context, dockerClient *docker.Docker, instanceID string) (types.Container, error) {
	matches, err := dockerClient.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			// Multiple filters get AND'd together
			filters.Arg("label", fmt.Sprintf("pgedge.instance.id=%s", instanceID)),
			filters.Arg("label", fmt.Sprintf("pgedge.component=%s", "postgres")),
		),
	})
	if err != nil {
		return types.Container{}, fmt.Errorf("failed to list containers: %w", err)
	}
	if len(matches) == 0 {
		return types.Container{}, fmt.Errorf("%w: %q", ErrNoPostgresContainer, instanceID)
	}
	return matches[0], nil
}

func PostgresContainerExec(ctx context.Context, w io.Writer, dockerClient *docker.Docker, instanceID string, cmd []string) error {
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

func GetPostgresService(ctx context.Context, dockerClient *docker.Docker, instanceID string) (swarm.Service, error) {
	matches, err := dockerClient.ServiceList(ctx, types.ServiceListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", fmt.Sprintf("pgedge.instance.id=%s", instanceID)),
			filters.Arg("label", fmt.Sprintf("pgedge.component=%s", "postgres")),
		),
	})
	if err != nil {
		return swarm.Service{}, fmt.Errorf("failed to list services: %w", err)
	}
	if len(matches) == 0 {
		return swarm.Service{}, fmt.Errorf("%w: %q", ErrNoPostgresService, instanceID)
	}
	return matches[0], nil
}

func PostgresServiceScale(ctx context.Context, dockerClient *docker.Docker, instanceID string, replicas int, timeout time.Duration) error {
	service, err := GetPostgresService(ctx, dockerClient, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get postgres service: %w", err)
	}
	err = dockerClient.ServiceScale(ctx, docker.ServiceScaleOptions{
		ServiceID:   service.ID,
		Scale:       uint64(replicas),
		Wait:        true,
		WaitTimeout: timeout,
	})
	if err != nil {
		return fmt.Errorf("failed to scale postgres service: %w", err)
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
