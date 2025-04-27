package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/task"
)

type CreatePgBackRestBackupInput struct {
	DatabaseID uuid.UUID                 `json:"database_id"`
	InstanceID uuid.UUID                 `json:"instance_id"`
	TaskID     uuid.UUID                 `json:"task_id"`
	Options    *pgbackrest.BackupOptions `json:"options"`
}

type CreatePgBackRestBackupOutput struct{}

func (a *Activities) ExecuteCreatePgBackRestBackup(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *CreatePgBackRestBackupInput,
) workflow.Future[*CreatePgBackRestBackupOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(a.Config.HostID.String()),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*CreatePgBackRestBackupOutput](ctx, options, a.CreatePgBackRestBackup, input)
}

func (a *Activities) CreatePgBackRestBackup(ctx context.Context, input *CreatePgBackRestBackupInput) (*CreatePgBackRestBackupOutput, error) {
	logger := activity.Logger(ctx).With("instance_id", input.InstanceID.String())
	logger.Info("running pgbackrest backup")

	orch, err := do.Invoke[database.Orchestrator](a.Injector)
	if err != nil {
		return nil, err
	}
	service, err := do.Invoke[*task.Service](a.Injector)
	if err != nil {
		return nil, err
	}
	taskLogWriter := task.NewTaskLogWriter(ctx, service, input.DatabaseID, input.TaskID)
	defer taskLogWriter.Close()

	err = orch.CreatePgBackRestBackup(ctx, taskLogWriter, input.InstanceID, input.Options)
	if err != nil {
		return nil, fmt.Errorf("failed to create pgBackRest backup: %w", err)
	}

	return &CreatePgBackRestBackupOutput{}, nil
}
