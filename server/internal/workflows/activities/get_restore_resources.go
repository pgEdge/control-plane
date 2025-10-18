package activities

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type GetRestoreResourcesInput struct {
	Spec          *database.InstanceSpec  `json:"spec"`
	TaskID        uuid.UUID               `json:"task_id"`
	RestoreConfig *database.RestoreConfig `json:"restore_config"`
}

type GetRestoreResourcesOutput struct {
	Resources        *database.InstanceResources `json:"resources"`
	RestoreResources *database.InstanceResources `json:"restore_resources"`
}

func (a *Activities) ExecuteGetRestoreResources(
	ctx workflow.Context,
	input *GetRestoreResourcesInput,
) workflow.Future[*GetRestoreResourcesOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(input.Spec.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*GetRestoreResourcesOutput](ctx, options, a.GetRestoreResources, input)
}

func (a *Activities) GetRestoreResources(ctx context.Context, input *GetRestoreResourcesInput) (*GetRestoreResourcesOutput, error) {
	logger := activity.Logger(ctx).With(
		"database_id", input.Spec.DatabaseID,
		"instance_id", input.Spec.InstanceID,
	)
	logger.Info("getting restore resources")

	resources, err := a.Orchestrator.GenerateInstanceResources(input.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to generate instance resources: %w", err)
	}

	restoreSpec := input.Spec.Clone()
	restoreSpec.RestoreConfig = input.RestoreConfig
	restoreResources, err := a.Orchestrator.GenerateInstanceRestoreResources(restoreSpec, input.TaskID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate restore resources: %w", err)
	}
	var repsetsJSON, rstTablesJSON string
	var capErr error
	if restoreSpec.RestoreConfig != nil {
		dbSvc, err := do.Invoke[*database.Service](a.Injector)
		if err != nil {
			return nil, err
		}
		instances, err := dbSvc.GetNodeInstances(ctx, restoreSpec.RestoreConfig.SourceDatabaseID, restoreSpec.RestoreConfig.SourceNodeName)
		if err != nil {
			logger.Info(fmt.Sprintf("warning: failed to get source instances for spock repset backup capture: %v", err))
		} else if len(instances) == 0 {
			logger.Info(fmt.Sprintf("warning: no source instances found for spock repset backup capture for database %s on node %s",
				restoreSpec.RestoreConfig.SourceDatabaseID, restoreSpec.RestoreConfig.SourceNodeName))
		} else {
			repsetsJSON, rstTablesJSON, capErr = a.Orchestrator.CaptureSpockRepsetBackup(ctx, instances[0].InstanceID)
			if capErr != nil {
				logger.Info(fmt.Sprintf("warning: failed to capture spock repset backup for instance %s: %v", instances[0].InstanceID, capErr))
				repsetsJSON, rstTablesJSON, capErr = a.Orchestrator.CaptureSpockRepsetBackup(ctx, input.Spec.InstanceID)
			}
		}

		if capErr != nil {
			// Not fatal: warn and continue. Restore can still proceed but replication sets won't be recreated.
			logger.Info(fmt.Sprintf("warning: failed to capture spock repset backup for instance %s: %v", input.Spec.InstanceID, capErr))
		} else {
			// Build a single payload object with both arrays so apply code has one source.
			payloadObj := map[string]json.RawMessage{
				"replication_sets":       json.RawMessage(repsetsJSON),
				"replication_set_tables": json.RawMessage(rstTablesJSON),
			}
			payloadBytes, mErr := json.Marshal(payloadObj)
			if mErr != nil {
				logger.Info(fmt.Sprintf("warning: failed to marshal spock backup payload for instance %s: %v", input.Spec.InstanceID, mErr))
			} else {
				backup := &database.SpockRepsetBackupResource{
					InstanceID: input.Spec.InstanceID,
					HostID:     input.Spec.HostID,
					Payload:    json.RawMessage(payloadBytes),
				}

				if resources != nil && resources.Instance != nil {
					resources.Instance.SpockRepsetBackup = backup
				}
				// Attach to restoreResources.Instance as well (if present)
				if restoreResources != nil && restoreResources.Instance != nil {
					restoreResources.Instance.SpockRepsetBackup = backup
				}
			}
		}
	}

	return &GetRestoreResourcesOutput{
		Resources:        resources,
		RestoreResources: restoreResources,
	}, nil
}
