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
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
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
	err = s.createWorkflow(ctx, t, databaseID.String(), s.workflows.UpdateDatabase, input)
	if err != nil {
		return nil, err
	}

	return t, nil
}

func (s *Service) UpdateDatabase(ctx context.Context, spec *database.Spec, forceUpdate bool) (*task.Task, error) {
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
	}
	err = s.createWorkflow(ctx, t, databaseID.String(), s.workflows.UpdateDatabase, input)
	if err != nil {
		return nil, err
	}

	return t, nil
}

func (s *Service) DeleteDatabase(ctx context.Context, databaseID uuid.UUID) (*task.Task, error) {
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
	err = s.createWorkflow(ctx, t, databaseID.String(), s.workflows.DeleteDatabase, input)
	if err != nil {
		return nil, err
	}

	return t, nil
}

func (s *Service) CreatePgBackRestBackup(
	ctx context.Context,
	databaseID uuid.UUID,
	nodeName string,
	backupFromStandby bool,
	instances []*InstanceHost,
	options *pgbackrest.BackupOptions,
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
		Options:           options,
	}
	instanceID := databaseID.String() + "-" + nodeName
	err = s.createWorkflow(ctx, t, instanceID, s.workflows.CreatePgBackRestBackup, input)
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
		RestoreConfig: restoreConfig,
		NodeTaskIDs:   nodeTaskIDs,
	}
	err = s.createWorkflow(ctx, t, databaseID.String(), s.workflows.PgBackRestRestore, input)
	if err != nil {
		return nil, nil, err
	}

	return t, nodeTasks, nil
}

func (s *Service) createWorkflow(ctx context.Context, t *task.Task, instanceID string, wf workflow.Workflow, args ...any) error {
	opts := client.WorkflowInstanceOptions{
		Queue:      core.Queue(s.cfg.HostID.String()),
		InstanceID: instanceID,
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
				Stringer("database_id", t.DatabaseID).
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

func (s *Service) ValidateVolumes(ctx context.Context, spec *database.Spec) *activities.ValidateVolumesOutput {
	if spec == nil {
		return &activities.ValidateVolumesOutput{
			Valid:  false,
			Errors: []string{"spec is nil"},
		}
	}

	databaseID := spec.DatabaseID
	opts := client.WorkflowInstanceOptions{
		Queue:      core.Queue(s.cfg.HostID.String()),
		InstanceID: databaseID.String(),
	}
	input := &ValidateVolumesInput{
		DatabaseID: databaseID,
		Spec:       spec,
	}

	instance, err := s.client.CreateWorkflowInstance(ctx, opts, s.workflows.ValidateSpec, input)
	if err != nil {
		s.logger.Error().Err(err).Str("database_id", databaseID.String()).Msg("Failed to create volume validation workflow")
		return &activities.ValidateVolumesOutput{
			Valid:  false,
			Errors: []string{fmt.Sprintf("failed to create workflow instance: %v", err)},
		}
	}

	output, err := client.GetWorkflowResult[*activities.ValidateVolumesOutput](ctx, s.client, instance, 5*time.Minute)
	if err != nil {
		s.logger.Error().Err(err).Str("database_id", databaseID.String()).Msg("Failed to get result from volume validation workflow")

		if output == nil {
			return &activities.ValidateVolumesOutput{
				Valid:  false,
				Errors: []string{fmt.Sprintf("failed to get workflow result: %v", err)},
			}
		}
	}

	return output
}
