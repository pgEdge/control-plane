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
	"github.com/rs/zerolog"
	"github.com/samber/do"
	"github.com/spf13/afero"
)

var _ resource.Resource = (*PgBackRestRestore)(nil)

const ResourceTypePgBackRestRestore resource.Type = "swarm.pgbackrest_restore"

func PgBackRestRestoreResourceIdentifier(instanceID uuid.UUID) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID.String(),
		Type: ResourceTypePgBackRestRestore,
	}
}

type PgBackRestRestore struct {
	DatabaseID uuid.UUID `json:"database_id"`
	HostID     uuid.UUID `json:"host_id"`
	InstanceID uuid.UUID `json:"instance_id"`
	TaskID     uuid.UUID `json:"task_id"`
	NodeName   string    `json:"node_name"`
	DataDirID  string    `json:"data_dir_id"`
	Options    []string  `json:"options"`
}

func (p *PgBackRestRestore) ResourceVersion() string {
	return "1"
}

func (p *PgBackRestRestore) DiffIgnore() []string {
	return nil
}

func (p *PgBackRestRestore) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeHost,
		ID:   p.HostID.String(),
	}
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

	t, err := taskSvc.GetTask(ctx, p.DatabaseID, p.TaskID)
	if err != nil {
		return fmt.Errorf("failed to get task %s: %w", p.TaskID, err)
	}
	t.Status = task.StatusRunning
	t.InstanceID = p.InstanceID
	t.HostID = p.HostID
	if err := taskSvc.UpdateTask(ctx, t); err != nil {
		return fmt.Errorf("failed to update task to running: %w", err)
	}
	handleError := func(cause error) error {
		t.SetFailed(cause)
		if err := taskSvc.UpdateTask(context.Background(), t); err != nil {
			logger.Err(err).
				Stringer("task_id", p.TaskID).
				Msg("failed to update task to failed")
		}
		return err
	}

	svcResource, err := resource.FromContext[*PostgresService](rc, PostgresServiceResourceIdentifier(p.InstanceID))
	if err != nil {
		return handleError(fmt.Errorf("failed to get postgres service resource from state: %w", err))
	}
	swarmService, err := dockerClient.ServiceInspect(ctx, svcResource.ServiceID)
	if err != nil {
		return handleError(fmt.Errorf("failed to inspect postgres service: %w", err))
	}
	dataDir, err := resource.FromContext[*filesystem.DirResource](rc, filesystem.DirResourceIdentifier(p.DataDirID))
	if err != nil {
		return handleError(fmt.Errorf("failed to get data dir resource from state: %w", err))
	}
	patroniCluster, err := resource.FromContext[*PatroniCluster](rc, PatroniClusterResourceIdentifier(p.NodeName))
	if err != nil {
		return handleError(fmt.Errorf("failed to get patroni cluster resource from state: %w", err))
	}

	err = dockerClient.ServiceScale(ctx, docker.ServiceScaleOptions{
		ServiceID:   swarmService.ID,
		Scale:       0,
		Wait:        true,
		WaitTimeout: time.Minute,
	})
	if err != nil {
		return handleError(fmt.Errorf("failed to scale down postgres service: %w", err))
	}

	// This resource exists to make it easy to remove the patroni namespace.
	// The namespace will automatically get recreated when Patroni starts up
	// again.
	if err := patroniCluster.Delete(ctx, rc); err != nil {
		return handleError(fmt.Errorf("failed to delete patroni cluster: %w", err))
	}

	// Remove the postmaster.pid file if it exists. This can happen if there was
	// an improper shutdown. We know that Postgres is not running because we
	// scaled down the service above.
	err = fs.Remove(filepath.Join(dataDir.Path, "pgdata", "postmaster.pid"))
	if err != nil && !errors.Is(err, afero.ErrFileNotFound) {
		return handleError(fmt.Errorf("failed to remove postmaster.pid file: %w", err))
	}

	var limits swarm.Limit
	if swarmService.Spec.TaskTemplate.Resources != nil && swarmService.Spec.TaskTemplate.Resources.Limits != nil {
		limits = *swarmService.Spec.TaskTemplate.Resources.Limits
	}
	containerSpec := swarmService.Spec.TaskTemplate.ContainerSpec
	containerID, err := dockerClient.ContainerRun(ctx, docker.ContainerRunOptions{
		Config: &container.Config{
			Image: containerSpec.Image,
			Labels: map[string]string{
				"pgedge.host.id":     p.HostID.String(),
				"pgedge.database.id": p.DatabaseID.String(),
				"pgedge.instance.id": p.InstanceID.String(),
				"pgedge.component":   "pgbackrest-restore",
			},
			Hostname:   containerSpec.Hostname,
			Entrypoint: PgBackRestRestoreCmd("restore", p.Options...).StringSlice(),
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
		return handleError(fmt.Errorf("failed to create pgbackrest restore container: %w", err))
	}
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
	err = dockerClient.ContainerLogs(ctx, taskLogger, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return handleError(fmt.Errorf("failed to get pgbackrest restore container logs: %w", err))
	}
	err = dockerClient.ContainerWait(ctx, containerID, container.WaitConditionNotRunning, 30*time.Second)
	if err != nil {
		return handleError(fmt.Errorf("error while waiting for pgbackrest restore container: %w", err))
	}

	err = dockerClient.ServiceScale(ctx, docker.ServiceScaleOptions{
		ServiceID:   swarmService.ID,
		Scale:       1,
		Wait:        true,
		WaitTimeout: time.Minute,
	})
	if err != nil {
		return handleError(fmt.Errorf("failed to scale up postgres service: %w", err))
	}

	t.SetCompleted()
	if err := taskSvc.UpdateTask(ctx, t); err != nil {
		return handleError(fmt.Errorf("failed to update task to completed: %w", err))
	}

	return nil
}

func (p *PgBackRestRestore) Update(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (p *PgBackRestRestore) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}
