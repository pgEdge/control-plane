package systemd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/samber/do"
	"github.com/spf13/afero"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var _ resource.Resource = (*PgBackRestRestore)(nil)

const ResourceTypePgBackRestRestore resource.Type = "systemd.pgbackrest_restore"

func PgBackRestRestoreResourceIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypePgBackRestRestore,
	}
}

type PgBackRestRestore struct {
	DatabaseID     string                 `json:"database_id"`
	HostID         string                 `json:"host_id"`
	InstanceID     string                 `json:"instance_id"`
	TaskID         uuid.UUID              `json:"task_id"`
	NodeName       string                 `json:"node_name"`
	Paths          database.InstancePaths `json:"paths"`
	RestoreOptions map[string]string      `json:"restore_options"`
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
		common.PgBackRestConfigIdentifier(p.InstanceID, pgbackrest.ConfigTypeRestore),
		common.PatroniClusterResourceIdentifier(p.NodeName),
		UnitResourceIdentifier(patroniServiceName(p.InstanceID), p.DatabaseID, p.HostID),
	}
}

func (p *PgBackRestRestore) TypeDependencies() []resource.Type {
	return nil
}

func (p *PgBackRestRestore) Refresh(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (p *PgBackRestRestore) Create(ctx context.Context, rc *resource.Context) error {
	orch, err := do.Invoke[database.Orchestrator](rc.Injector)
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
	if err != nil {
		return err
	}

	handleError := func(cause error) error {
		p.failTask(logger, taskSvc, t, cause)
		return cause
	}

	err = p.stopPostgres(ctx, rc, orch, fs)
	if err != nil {
		return handleError(err)
	}

	err = p.runRestoreCmd(ctx, orch, logger, taskSvc)
	if err != nil {
		return handleError(err)
	}

	err = p.renameDataDir(fs)
	if err != nil {
		return handleError(err)
	}

	err = orch.StartInstance(ctx, p.InstanceID)
	if err != nil {
		return handleError(fmt.Errorf("failed to start patroni after restore: %w", err))
	}

	err = p.completeTask(ctx, taskSvc, t)
	if err != nil {
		return handleError(err)
	}

	return nil
}

func (p *PgBackRestRestore) startTask(ctx context.Context, taskSvc *task.Service) (*task.Task, error) {
	t, err := taskSvc.GetTask(ctx, task.ScopeDatabase, p.DatabaseID, p.TaskID)
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
	orch database.Orchestrator,
	fs afero.Fs,
) error {
	patroniCluster, err := resource.FromContext[*common.PatroniCluster](rc, common.PatroniClusterResourceIdentifier(p.NodeName))
	if err != nil {
		return fmt.Errorf("failed to get patroni cluster resource from state: %w", err)
	}

	err = orch.StopInstance(ctx, p.InstanceID)
	if err != nil && !errors.Is(err, ErrUnitNotFound) {
		return fmt.Errorf("failed to stop patroni: %w", err)
	}

	// This resource exists to make it easy to remove the patroni namespace.
	// The namespace will automatically get recreated when Patroni starts up
	// again.
	if err := patroniCluster.Delete(ctx, rc); err != nil {
		return fmt.Errorf("failed to delete patroni cluster: %w", err)
	}

	// Remove the postmaster.pid file if it exists. This can happen if there was
	// an improper shutdown. We know that Postgres is not running because we
	// stopped the unit above.
	err = fs.Remove(filepath.Join(p.Paths.Instance.PgData(), "postmaster.pid"))
	if err != nil && !errors.Is(err, afero.ErrFileNotFound) {
		return fmt.Errorf("failed to remove postmaster.pid file: %w", err)
	}

	return nil
}

func (p *PgBackRestRestore) runRestoreCmd(
	ctx context.Context,
	orch database.Orchestrator,
	logger zerolog.Logger,
	taskSvc *task.Service,
) error {
	restoreOptions := utils.BuildOptionArgs(p.RestoreOptions)
	opts := append([]string{"--log-timestamp=n"}, restoreOptions...)
	cmd := p.Paths.PgBackRestRestoreCmd("restore", opts...).StringSlice()
	taskLogger := task.NewTaskLogWriter(ctx, taskSvc, task.ScopeDatabase, p.DatabaseID, p.TaskID)

	err := orch.ExecuteInstanceCommand(ctx, taskLogger, p.DatabaseID, p.InstanceID, cmd...)
	if err != nil {
		return fmt.Errorf("failed to execute pgbackrest restore command: %w", err)
	}

	return nil
}

func (p *PgBackRestRestore) renameDataDir(fs afero.Fs) error {
	if err := fs.Rename(p.Paths.Instance.PgData(), p.Paths.Instance.PgDataRestore()); err != nil {
		return fmt.Errorf("failed to rename pgdata for restore: %w", err)
	}

	return nil
}

func (p *PgBackRestRestore) Update(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (p *PgBackRestRestore) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}
