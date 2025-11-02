package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type PersistPlanSummariesInput struct {
	DatabaseID string                 `json:"database_id"`
	TaskID     uuid.UUID              `json:"task_id"`
	Plans      []resource.PlanSummary `json:"plans"`
}

type PersistPlanSummariesOutput struct{}

func (a *Activities) ExecutePersistPlanSummaries(
	ctx workflow.Context,
	input *PersistPlanSummariesInput,
) workflow.Future[*PersistPlanSummariesOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*PersistPlanSummariesOutput](ctx, options, a.PersistPlanSummaries, input)
}

func (a *Activities) PersistPlanSummaries(ctx context.Context, input *PersistPlanSummariesInput) (*PersistPlanSummariesOutput, error) {
	service, err := do.Invoke[*resource.Service](a.Injector)
	if err != nil {
		return nil, err
	}

	err = service.PersistPlanSummaries(ctx, input.DatabaseID, input.TaskID, input.Plans)
	if err != nil {
		return nil, fmt.Errorf("failed to persist plans: %w", err)
	}

	return &PersistPlanSummariesOutput{}, nil
}
