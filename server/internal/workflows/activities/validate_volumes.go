package activities

import (
	"context"
	"errors"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/samber/do"
)

type ValidateVolumesInput struct {
	DatabaseID uuid.UUID              `json:"database_id"`
	Spec       *database.InstanceSpec `json:"spec"`
}

type ValidateVolumesOutput struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

func (a *Activities) ExecuteValidateVolumes(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *ValidateVolumesInput,
) workflow.Future[*ValidateVolumesOutput] {
	options := workflow.ActivityOptions{
		Queue: workflow.Queue(hostID.String()),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*ValidateVolumesOutput](ctx, options, a.ValidateVolumes, input)
}

func (a *Activities) ValidateVolumes(ctx context.Context, input *ValidateVolumesInput) (*ValidateVolumesOutput, error) {
	logger := activity.Logger(ctx)

	fail := func(err error, msg string) (*ValidateVolumesOutput, error) {
		logger.Error(msg, "error", err)
		return &ValidateVolumesOutput{
			Valid:  false,
			Errors: []string{msg + ": " + err.Error()},
		}, err
	}

	if input == nil {
		return fail(errors.New("input is nil"), "input cannot be nil")
	}
	if input.DatabaseID == uuid.Nil {
		return fail(errors.New("invalid UUID"), "invalid database ID")
	}

	logger = logger.With("database_id", input.DatabaseID.String())
	logger.Info("starting volume validation")

	if input.Spec == nil {
		return fail(errors.New("spec is nil"), "spec is required for volume validation")
	}

	orch, err := do.Invoke[database.Orchestrator](a.Injector)
	if err != nil {
		return fail(err, "failed to resolve orchestrator")
	}

	result, err := orch.ValidateVolumes(ctx, input.Spec)
	if err != nil {
		return fail(err, "volume validation failed")
	}

	logger.Info("volume validation completed", "success", result.Success)
	return &ValidateVolumesOutput{
		Valid: result.Success,
	}, nil
}
