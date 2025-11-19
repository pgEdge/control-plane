package workflows

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/backend"
	"github.com/cschleiden/go-workflows/client"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var ErrDuplicateWorkflow = errors.New("duplicate workflow already in progress")

type Service struct {
	cfg       config.Config
	client    *client.Client
	taskSvc   *task.Service
	logger    zerolog.Logger
	workflows *Workflows
}

func NewService(
	cfg config.Config,
	client *client.Client,
	taskSvc *task.Service,
	workflows *Workflows,
) *Service {
	return &Service{
		cfg:       cfg,
		client:    client,
		taskSvc:   taskSvc,
		workflows: workflows,
	}
}

func (s *Service) CreateDatabase(ctx context.Context, spec *database.Spec) (*task.Task, error) {
	databaseID := spec.DatabaseID
	// Clear out any old tasks. This can happen if you were to recreate a
	// database with the same ID.
	if err := s.taskSvc.DeleteAllTasks(ctx, databaseID); err != nil {
		return nil, fmt.Errorf("failed to delete old task logs: %w", err)
	}
	t, err := s.taskSvc.CreateTask(ctx, task.Options{
		DatabaseID: databaseID,
		Type:       task.TypeCreate,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create new task: %w", err)
	}
	input := &UpdateDatabaseInput{
		TaskID: t.TaskID,
		Spec:   spec,
	}
	err = s.createWorkflow(ctx, t, s.workflows.UpdateDatabase, input)
	if err != nil {
		return nil, err
	}

	return t, nil
}

func (s *Service) UpdateDatabase(ctx context.Context, spec *database.Spec, forceUpdate bool, removeHosts ...string) (*task.Task, error) {
	databaseID := spec.DatabaseID
	t, err := s.taskSvc.CreateTask(ctx, task.Options{
		DatabaseID: databaseID,
		Type:       task.TypeUpdate,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create new task: %w", err)
	}
	input := &UpdateDatabaseInput{
		TaskID:      t.TaskID,
		Spec:        spec,
		ForceUpdate: forceUpdate,
		RemoveHosts: removeHosts,
	}
	err = s.createWorkflow(ctx, t, s.workflows.UpdateDatabase, input)
	if err != nil {
		return nil, err
	}

	return t, nil
}

func (s *Service) DeleteDatabase(ctx context.Context, databaseID string) (*task.Task, error) {
	t, err := s.taskSvc.CreateTask(ctx, task.Options{
		DatabaseID: databaseID,
		Type:       task.TypeDelete,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create new task: %w", err)
	}
	input := &DeleteDatabaseInput{
		DatabaseID: databaseID,
		TaskID:     t.TaskID,
	}
	err = s.createWorkflow(ctx, t, s.workflows.DeleteDatabase, input)
	if err != nil {
		return nil, err
	}

	return t, nil
}

func (s *Service) CreatePgBackRestBackup(
	ctx context.Context,
	databaseID string,
	nodeName string,
	backupFromStandby bool,
	instances []*InstanceHost,
	backupOptions *pgbackrest.BackupOptions,
) (*task.Task, error) {
	t, err := s.taskSvc.CreateTask(ctx, task.Options{
		DatabaseID: databaseID,
		Type:       task.TypeNodeBackup,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create new task: %w", err)
	}
	input := &CreatePgBackRestBackupInput{
		DatabaseID:        databaseID,
		NodeName:          nodeName,
		TaskID:            t.TaskID,
		BackupFromStandby: backupFromStandby,
		Instances:         instances,
		BackupOptions:     backupOptions,
	}
	err = s.createWorkflow(ctx, t, s.workflows.CreatePgBackRestBackup, input)
	if err != nil {
		return nil, err
	}

	return t, nil
}

func (s *Service) PgBackRestRestore(
	ctx context.Context,
	spec *database.Spec,
	targetNodes []string,
	restoreConfig *database.RestoreConfig,
) (*task.Task, []*task.Task, error) {
	databaseID := spec.DatabaseID

	t, err := s.taskSvc.CreateTask(ctx, task.Options{
		DatabaseID: databaseID,
		Type:       task.TypeRestore,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create new task: %w", err)
	}

	nodeTaskIDs := map[string]uuid.UUID{}
	allTasks := []*task.Task{t}
	nodeTasks := []*task.Task{}
	for _, node := range targetNodes {
		nt, err := s.taskSvc.CreateTask(ctx, task.Options{
			ParentID:   t.TaskID,
			DatabaseID: databaseID,
			NodeName:   node,
			Type:       task.TypeNodeRestore,
		})
		if err != nil {
			s.abortTasks(ctx, allTasks...)
			return nil, nil, fmt.Errorf("failed to create new task: %w", err)
		}
		allTasks = append(allTasks, nt)
		nodeTasks = append(nodeTasks, nt)
		nodeTaskIDs[node] = nt.TaskID
	}

	input := &PgBackRestRestoreInput{
		TaskID:        t.TaskID,
		Spec:          spec,
		TargetNodes:   targetNodes,
		RestoreConfig: restoreConfig.Clone(),
		NodeTaskIDs:   nodeTaskIDs,
	}
	err = s.createWorkflow(ctx, t, s.workflows.PgBackRestRestore, input)
	if err != nil {
		return nil, nil, err
	}

	return t, nodeTasks, nil
}

func (s *Service) createWorkflow(ctx context.Context, t *task.Task, wf workflow.Workflow, args ...any) error {
	opts := client.WorkflowInstanceOptions{
		Queue:      utils.HostQueue(s.cfg.HostID),
		InstanceID: uuid.NewString(),
	}
	instance, err := s.client.CreateWorkflowInstance(ctx, opts, wf, args...)
	if err != nil {
		s.abortTasks(ctx, t)
		return s.translateCreateErr(err)
	}

	t.WorkflowExecutionID = instance.ExecutionID
	t.WorkflowInstanceID = instance.InstanceID

	err = s.taskSvc.UpdateTask(ctx, t)
	if err != nil {
		return fmt.Errorf("failed to update task: %w", err)
	}

	return nil
}

func (s *Service) abortTasks(ctx context.Context, tasks ...*task.Task) {
	for _, t := range tasks {
		err := s.taskSvc.DeleteTask(ctx, t.DatabaseID, t.TaskID)
		if err != nil {
			s.logger.Err(err).
				Str("database_id", t.DatabaseID).
				Stringer("task_id", t.TaskID).
				Msg("failed to delete aborted task")
		}
	}
}

func (s *Service) translateCreateErr(err error) error {
	if errors.Is(err, backend.ErrInstanceAlreadyExists) {
		return ErrDuplicateWorkflow
	}
	return fmt.Errorf("failed to create workflow instance: %w", err)
}

func (s *Service) ValidateSpec(ctx context.Context, input *ValidateSpecInput) (*ValidateSpecOutput, error) {
	if input == nil || input.Spec == nil {
		return nil, errors.New("spec is nil")
	}

	databaseID := input.Spec.DatabaseID
	opts := client.WorkflowInstanceOptions{
		Queue:      utils.HostQueue(s.cfg.HostID),
		InstanceID: uuid.NewString(),
	}

	instance, err := s.client.CreateWorkflowInstance(ctx, opts, s.workflows.ValidateSpec, input)
	if err != nil {
		s.logger.Error().Err(err).Str("database_id", databaseID).Msg("failed to create spec validation workflow")
		return nil, fmt.Errorf("failed to create workflow instance: %w", err)
	}

	output, err := client.GetWorkflowResult[*ValidateSpecOutput](ctx, s.client, instance, 5*time.Minute)
	if err != nil {
		s.logger.Error().Err(err).Str("database_id", databaseID).Msg("spec validation workflow failed")
		return nil, fmt.Errorf("spec validation workflow failed: %w", err)
	}

	return output, nil
}

func (s *Service) RestartInstance(ctx context.Context, input *RestartInstanceInput) (*task.Task, error) {
	t, err := s.taskSvc.CreateTask(ctx, task.Options{
		DatabaseID: input.DatabaseID,
		InstanceID: input.InstanceID,
		Type:       task.TypeRestartInstance,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create new task: %w", err)
	}
	input.TaskID = t.TaskID
	err = s.createWorkflow(ctx, t, s.workflows.RestartInstance, input)
	if err != nil {
		return nil, err
	}

	return t, nil
}

func (s *Service) StopInstance(ctx context.Context, input *StopInstanceInput) (*task.Task, error) {
	t, err := s.taskSvc.CreateTask(ctx, task.Options{
		DatabaseID: input.DatabaseID,
		InstanceID: input.InstanceID,
		HostID:     input.HostID,
		Type:       task.TypeStopInstance,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create new task: %w", err)
	}
	input.TaskID = t.TaskID
	err = s.createWorkflow(ctx, t, s.workflows.StopInstance, input)
	if err != nil {
		return nil, err
	}

	return t, nil
}

func (s *Service) StartInstance(ctx context.Context, input *StartInstanceInput) (*task.Task, error) {
	t, err := s.taskSvc.CreateTask(ctx, task.Options{
		DatabaseID: input.DatabaseID,
		InstanceID: input.InstanceID,
		HostID:     input.HostID,
		Type:       task.TypeStartInstance,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create new task: %w", err)
	}
	input.TaskID = t.TaskID
	err = s.createWorkflow(ctx, t, s.workflows.StartInstance, input)
	if err != nil {
		return nil, err
	}

	return t, nil
}

func (s *Service) CancelDatabaseTask(ctx context.Context, DatabaseID string, taskID uuid.UUID) (*task.Task, error) {
	t, err := s.taskSvc.GetTask(ctx, DatabaseID, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve task from database : %w", err)
	}
	if t.WorkflowInstanceID == "" {
		return nil, fmt.Errorf("no worflow instances associated with task")
	}

	t.Status = task.StatusCanceling
	err = s.taskSvc.UpdateTask(ctx, t)
	if err != nil {
		return t, fmt.Errorf("failed to update task status to canceling  %w", err)
	}
	_ = s.taskSvc.AddLogEntry(ctx, DatabaseID, taskID, task.LogEntry{
		Message: "task is canceling",
		Fields:  map[string]any{"status": "canceling"},
	})

	wrkflw_instance := core.WorkflowInstance{
		InstanceID:  t.WorkflowInstanceID,
		ExecutionID: t.WorkflowExecutionID,
	}

	if err := s.client.CancelWorkflowInstance(ctx, &wrkflw_instance); err != nil {
		return nil, fmt.Errorf("failed to cancel workflow instance %w", err)
	}

	return t, nil

}

func (s *Service) SwitchoverDatabaseNode(ctx context.Context, input *SwitchoverInput) (*task.Task, error) {
	t, err := s.taskSvc.CreateTask(ctx, task.Options{
		DatabaseID: input.DatabaseID,
		InstanceID: input.CandidateInstanceID,
		NodeName:   input.NodeName,
		Type:       task.TypeSwitchover,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create new task: %w", err)
	}

	input.TaskID = t.TaskID

	if err := s.createWorkflow(ctx, t, s.workflows.Switchover, input); err != nil {
		return nil, err
	}

	return t, nil
}

func (s *Service) FailoverDatabaseNode(ctx context.Context, input *FailoverInput) (*task.Task, error) {
	t, err := s.taskSvc.CreateTask(ctx, task.Options{
		DatabaseID: input.DatabaseID,
		InstanceID: input.CandidateInstanceID,
		NodeName:   input.NodeName,
		Type:       task.TypeFailover,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create new task: %w", err)
	}

	input.TaskID = t.TaskID

	err = s.createWorkflow(ctx, t, s.workflows.Failover, input)
	if err != nil {
		return nil, err
	}

	return t, nil
}
