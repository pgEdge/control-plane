package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type GetScriptResultsInput struct {
	DatabaseID  string                `json:"database_id"`
	NodeName    string                `json:"node_name"`
	ScriptNames []database.ScriptName `json:"script_names"`
}

type GetScriptResultsOutput struct {
	Succeeded map[database.ScriptName]bool `json:"succeeded"`
}

func (a *Activities) ExecuteGetScriptResults(
	ctx workflow.Context,
	input *GetScriptResultsInput,
) workflow.Future[*GetScriptResultsOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.AnyQueue(),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*GetScriptResultsOutput](ctx, options, a.GetScriptResults, input)
}

func (a *Activities) GetScriptResults(ctx context.Context, input *GetScriptResultsInput) (*GetScriptResultsOutput, error) {
	logger := activity.Logger(ctx).With(
		"database_id", input.DatabaseID,
		"node_name", input.NodeName,
	)
	logger.Debug("getting script results")

	succeeded := make(map[database.ScriptName]bool, len(input.ScriptNames))
	for _, scriptName := range input.ScriptNames {
		result, err := a.DatabaseService.GetScriptResult(ctx, input.DatabaseID, scriptName, input.NodeName)
		if err != nil {
			return nil, fmt.Errorf("failed to get script result: %w", err)
		}
		succeeded[scriptName] = result.Succeeded
	}

	return &GetScriptResultsOutput{
		Succeeded: succeeded,
	}, nil
}
