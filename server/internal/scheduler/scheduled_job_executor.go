package scheduler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/workflows"
)

type WorkflowExecutor interface {
	Execute(ctx context.Context, workflowName string, args map[string]interface{}) error
}

type DefaultWorkflowExecutor struct {
	workflowSvc *workflows.Service
}

func NewDefaultWorkflowExecutor(wf *workflows.Service) *DefaultWorkflowExecutor {
	return &DefaultWorkflowExecutor{
		workflowSvc: wf,
	}
}

func (e *DefaultWorkflowExecutor) Execute(ctx context.Context, workflowName string, args map[string]interface{}) error {
	switch workflowName {
	case WorkflowCreatePgBackRestBackup:
		var input CreatePgBackRestBackupScheduleInput
		if err := decodeArgs(args, &input); err != nil {
			return err
		}

		_, err := e.workflowSvc.CreatePgBackRestBackup(ctx,
			input.DatabaseID,
			input.NodeName,
			false,
			[]*workflows.InstanceHost{
				{
					InstanceID: input.InstanceID,
					HostID:     input.HostID,
				},
			},
			&pgbackrest.BackupOptions{
				Type: pgbackrest.BackupType(input.Type),
			},
		)
		return err

	default:
		return fmt.Errorf("unknown workflow: %s", workflowName)
	}
}

func decodeArgs(args map[string]interface{}, out interface{}) error {
	res, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("failed to marshal args: %w", err)
	}
	if err := json.Unmarshal(res, out); err != nil {
		return fmt.Errorf("failed to unmarshal args into %T: %w", out, err)
	}
	return nil
}

// Schedule input struct matching scheduled job args
type CreatePgBackRestBackupScheduleInput struct {
	DatabaseID string `json:"database_id"`
	InstanceID string `json:"instance_id"`
	HostID     string `json:"host_id"`
	NodeName   string `json:"node_name"`
	Type       string `json:"type"`
	TaskID     string `json:"task_id"`
}
