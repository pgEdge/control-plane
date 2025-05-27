package task

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var ErrTaskNotFound = fmt.Errorf("task not found")

type Service struct {
	Store *Store
}

func NewService(store *Store) *Service {
	return &Service{
		Store: store,
	}
}

func (s *Service) CreateTask(ctx context.Context, opts Options) (*Task, error) {
	task, err := NewTask(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}
	err = s.Store.Task.Create(&StoredTask{
		Task: task,
	}).Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	return task, nil
}

func (s *Service) UpdateTask(ctx context.Context, task *Task) error {
	stored, err := s.Store.Task.GetByKey(task.DatabaseID, task.TaskID).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return ErrTaskNotFound
	} else if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}
	stored.Task = task

	err = s.Store.Task.Update(stored).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}

	return nil
}

func (s *Service) GetTask(ctx context.Context, databaseID, taskID uuid.UUID) (*Task, error) {
	stored, err := s.Store.Task.GetByKey(databaseID, taskID).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrTaskNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	return stored.Task, nil
}

func (s *Service) GetTasks(ctx context.Context, databaseID uuid.UUID, options TaskListOptions) ([]*Task, error) {
	stored, err := s.Store.Task.GetAllByDatabaseID(databaseID, options).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrTaskNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to get tasks: %w", err)
	}

	tasks := make([]*Task, len(stored))
	for i, s := range stored {
		tasks[i] = s.Task
	}

	return tasks, nil
}

func (s *Service) DeleteTask(ctx context.Context, databaseID, taskID uuid.UUID) error {
	deleted, err := s.Store.Task.Delete(databaseID, taskID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}
	if deleted == 0 {
		return ErrTaskNotFound
	}
	if err := s.DeleteTaskLogs(ctx, databaseID, taskID); err != nil {
		return err
	}
	return nil
}

func (s *Service) DeleteAllTasks(ctx context.Context, databaseID uuid.UUID) error {
	_, err := s.Store.Task.DeleteByDatabaseID(databaseID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete tasks: %w", err)
	}
	if err := s.DeleteAllTaskLogs(ctx, databaseID); err != nil {
		return err
	}
	return nil
}

func (s *Service) AddLogLine(ctx context.Context, databaseID, taskID uuid.UUID, line string) error {
	lineID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("failed to create line ID: %w", err)
	}
	stored := &StoredTaskLogLine{
		DatabaseID: databaseID,
		TaskID:     taskID,
		LineID:     lineID,
		Line:       utils.Clean(line), // remove all control characters
	}
	err = s.Store.TaskLogLine.Put(stored).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to create task log line: %w", err)
	}

	return nil
}

func (s *Service) GetTaskLog(ctx context.Context, databaseID, taskID uuid.UUID, options TaskLogOptions) (*TaskLog, error) {
	stored, err := s.Store.TaskLogLine.GetAllByTaskID(databaseID, taskID, options).Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get task log: %w", err)
	}

	log := &TaskLog{
		DatabaseID: databaseID,
		TaskID:     taskID,
	}
	for i := len(stored) - 1; i >= 0; i-- {
		s := stored[i]
		log.Lines = append(log.Lines, s.Line)
	}
	if len(stored) > 0 {
		log.LastLineID = stored[len(stored)-1].LineID
	}

	return log, nil
}

func (s *Service) DeleteTaskLogs(ctx context.Context, databaseID, taskID uuid.UUID) error {
	_, err := s.Store.TaskLogLine.DeleteByTaskID(databaseID, taskID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete task logs: %w", err)
	}
	return nil
}

func (s *Service) DeleteAllTaskLogs(ctx context.Context, databaseID uuid.UUID) error {
	_, err := s.Store.TaskLogLine.DeleteByDatabaseID(databaseID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete task logs: %w", err)
	}
	return nil
}
