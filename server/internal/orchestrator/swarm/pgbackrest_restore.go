package swarm

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/rs/zerolog"
	"github.com/samber/do"
	"github.com/spf13/afero"
)

var _ resource.Resource = (*PgBackRestRestore)(nil)

const ResourceTypePgBackRestRestore resource.Type = "swarm.pgbackrest_restore"

func PgBackRestRestoreResourceIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypePgBackRestRestore,
	}
}

type PgBackRestRestore struct {
	DatabaseID     string            `json:"database_id"`
	HostID         string            `json:"host_id"`
	InstanceID     string            `json:"instance_id"`
	TaskID         uuid.UUID         `json:"task_id"`
	NodeName       string            `json:"node_name"`
	DataDirID      string            `json:"data_dir_id"`
	RestoreOptions map[string]string `json:"restore_options"`
}

func (p *PgBackRestRestore) ResourceVersion() string {
	return "1"
}

func (p *PgBackRestRestore) DiffIgnore() []string {
	return nil
}

func (p *PgBackRestRestore) Executor() resource.Executor {
	return resource.HostExecutor(p.HostID)
}

func (p *PgBackRestRestore) Identifier() resource.Identifier {
	return PgBackRestRestoreResourceIdentifier(p.InstanceID)
}

func (p *PgBackRestRestore) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		filesystem.DirResourceIdentifier(p.DataDirID),
		PostgresServiceResourceIdentifier(p.InstanceID),
		PostgresServiceSpecResourceIdentifier(p.InstanceID),
		PgBackRestConfigIdentifier(p.InstanceID, PgBackRestConfigTypeRestore),
		PatroniClusterResourceIdentifier(p.NodeName),
		ScaleServiceResourceIdentifier(p.InstanceID, ScaleDirectionDOWN),
	}
}

func (p *PgBackRestRestore) Refresh(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (p *PgBackRestRestore) Create(ctx context.Context, rc *resource.Context) error {
	dockerClient, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}
	taskSvc, err := do.Invoke[*task.Service](rc.Injector)
	if err != nil {
		return err
	}
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	t, err := p.startTask(ctx, taskSvc)
	handleError := func(cause error) error {
		p.failTask(logger, taskSvc, t, cause)
		return err
	}

	svcResource, err := resource.FromContext[*PostgresService](rc, PostgresServiceResourceIdentifier(p.InstanceID))
	if err != nil {
		return handleError(fmt.Errorf("failed to get postgres service resource from state: %w", err))
	}

	err = p.stopPostgres(ctx, rc, dockerClient, fs, svcResource)
	if err != nil {
		return handleError(err)
	}

	containerID, err := p.runRestoreContainer(ctx, dockerClient, svcResource)
	if err != nil {
		return handleError(err)
	}

	err = p.streamLogsAndWait(ctx, dockerClient, logger, taskSvc, containerID)
	if err != nil {
		return handleError(err)
	}

	err = p.completeTask(ctx, taskSvc, t)
	if err != nil {
		return handleError(err)
	}

	return nil
}

func (p *PgBackRestRestore) startTask(ctx context.Context, taskSvc *task.Service) (*task.Task, error) {
	t, err := taskSvc.GetTask(ctx, p.DatabaseID, p.TaskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task %s: %w", p.TaskID, err)
	}
	t.Status = task.StatusRunning
	t.InstanceID = p.InstanceID
	t.HostID = p.HostID
	if err := taskSvc.UpdateTask(ctx, t); err != nil {
		return nil, fmt.Errorf("failed to update task to running: %w", err)
	}

	return t, err
}

func (p *PgBackRestRestore) failTask(
	logger zerolog.Logger,
	taskSvc *task.Service,
	t *task.Task,
	cause error,
) {
	t.SetFailed(cause)
	if err := taskSvc.UpdateTask(context.Background(), t); err != nil {
		logger.Err(err).
			Stringer("task_id", p.TaskID).
			Msg("failed to update task to failed")
	}
}

func (p *PgBackRestRestore) completeTask(
	ctx context.Context,
	taskSvc *task.Service,
	t *task.Task,
) error {
	t.SetCompleted()
	if err := taskSvc.UpdateTask(ctx, t); err != nil {
		return fmt.Errorf("failed to update task to completed: %w", err)
	}

	return nil
}

func (p *PgBackRestRestore) stopPostgres(
	ctx context.Context,
	rc *resource.Context,
	dockerClient *docker.Docker,
	fs afero.Fs,
	svcResource *PostgresService,
) error {
	dataDir, err := resource.FromContext[*filesystem.DirResource](rc, filesystem.DirResourceIdentifier(p.DataDirID))
	if err != nil {
		return fmt.Errorf("failed to get data dir resource from state: %w", err)
	}
	patroniCluster, err := resource.FromContext[*PatroniCluster](rc, PatroniClusterResourceIdentifier(p.NodeName))
	if err != nil {
		return fmt.Errorf("failed to get patroni cluster resource from state: %w", err)
	}

	// This resource exists to make it easy to remove the patroni namespace.
	// The namespace will automatically get recreated when Patroni starts up
	// again.
	if err := patroniCluster.Delete(ctx, rc); err != nil {
		return fmt.Errorf("failed to delete patroni cluster: %w", err)
	}

	// Remove the postmaster.pid file if it exists. This can happen if there was
	// an improper shutdown. We know that Postgres is not running because we
	// scaled down the service above.
	err = fs.Remove(filepath.Join(dataDir.FullPath, "pgdata", "postmaster.pid"))
	if err != nil && !errors.Is(err, afero.ErrFileNotFound) {
		return fmt.Errorf("failed to remove postmaster.pid file: %w", err)
	}

	return nil
}

func (p *PgBackRestRestore) runRestoreContainer(
	ctx context.Context,
	dockerClient *docker.Docker,
	svcResource *PostgresService,
) (string, error) {
	swarmService, err := dockerClient.ServiceInspect(ctx, svcResource.ServiceID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect postgres service: %w", err)
	}
	var limits swarm.Limit
	if swarmService.Spec.TaskTemplate.Resources != nil && swarmService.Spec.TaskTemplate.Resources.Limits != nil {
		limits = *swarmService.Spec.TaskTemplate.Resources.Limits
	}
	containerSpec := swarmService.Spec.TaskTemplate.ContainerSpec
	restoreOptions := utils.BuildOptionArgs(p.RestoreOptions)
	opts := append([]string{"--log-timestamp=n"}, restoreOptions...)
	containerID, err := dockerClient.ContainerRun(ctx, docker.ContainerRunOptions{
		Config: &container.Config{
			Image: containerSpec.Image,
			Labels: map[string]string{
				"pgedge.host.id":     p.HostID,
				"pgedge.database.id": p.DatabaseID,
				"pgedge.instance.id": p.InstanceID,
				"pgedge.component":   "pgbackrest-restore",
			},
			Hostname:   containerSpec.Hostname,
			Entrypoint: PgBackRestRestoreCmd("restore", opts...).StringSlice(),
		},
		Host: &container.HostConfig{
			Mounts: containerSpec.Mounts,
			Resources: container.Resources{
				Memory:   limits.MemoryBytes,
				NanoCPUs: limits.NanoCPUs,
			},
		},
		Name: fmt.Sprintf("pgbackrest-restore-%s", p.InstanceID),
	})
	if err != nil {
		return "", fmt.Errorf("failed to create pgbackrest restore container: %w", err)
	}

	return containerID, nil
}

func (p *PgBackRestRestore) streamLogsAndWait(
	ctx context.Context,
	dockerClient *docker.Docker,
	logger zerolog.Logger,
	taskSvc *task.Service,
	containerID string,
) error {
	defer func() {
		err := dockerClient.ContainerRemove(ctx, containerID, container.RemoveOptions{
			// Using force here to so that we can safely retry this operation.
			Force: true,
		})
		if err != nil {
			logger.Err(err).
				Str("container_id", containerID).
				Msg("failed to remove pgbackrest restore container")
		}
	}()
	taskLogger := task.NewTaskLogWriter(ctx, taskSvc, p.DatabaseID, p.TaskID)
	// The follow: true means that this will block until the container exits.
	err := dockerClient.ContainerLogs(ctx, taskLogger, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return fmt.Errorf("failed to get pgbackrest restore container logs: %w", err)
	}
	err = dockerClient.ContainerWait(ctx, containerID, container.WaitConditionNotRunning, 30*time.Second)
	if err != nil {
		return fmt.Errorf("error while waiting for pgbackrest restore container: %w", err)
	}

	return nil
}

func (p *PgBackRestRestore) Update(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (p *PgBackRestRestore) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}
