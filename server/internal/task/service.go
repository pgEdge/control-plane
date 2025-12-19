package task

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

var ErrTaskNotFound = errors.New("task not found")

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

func (s *Service) GetTask(ctx context.Context, databaseID string, taskID uuid.UUID) (*Task, error) {
	stored, err := s.Store.Task.GetByKey(databaseID, taskID).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrTaskNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	return stored.Task, nil
}

func (s *Service) GetTasks(ctx context.Context, databaseID string, options TaskListOptions) ([]*Task, error) {
	if options.Type == "" && options.NodeName == "" && len(options.Statuses) == 0 {
		return s.getTasks(ctx, databaseID, options)
	}

	return s.getTasksFiltered(ctx, databaseID, options)
}

func (s *Service) DeleteTask(ctx context.Context, databaseID string, taskID uuid.UUID) error {
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

func (s *Service) DeleteAllTasks(ctx context.Context, databaseID string) error {
	_, err := s.Store.Task.DeleteByDatabaseID(databaseID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete tasks: %w", err)
	}
	if err := s.DeleteAllTaskLogs(ctx, databaseID); err != nil {
		return err
	}
	return nil
}

type LogEntry struct {
	Timestamp time.Time
	Message   string
	Fields    map[string]any
}

func (s *Service) AddLogEntry(ctx context.Context, databaseID string, taskID uuid.UUID, entry LogEntry) error {
	entryID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("failed to create entry ID: %w", err)
	}
	timestamp := entry.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	stored := &StoredTaskLogEntry{
		DatabaseID: databaseID,
		TaskID:     taskID,
		EntryID:    entryID,
		Timestamp:  timestamp,
		Message:    entry.Message,
		Fields:     entry.Fields,
	}
	err = s.Store.TaskLogMessage.Put(stored).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to create task log entry: %w", err)
	}

	return nil
}

func (s *Service) GetTaskLog(ctx context.Context, databaseID string, taskID uuid.UUID, options TaskLogOptions) (*TaskLog, error) {
	stored, err := s.Store.TaskLogMessage.GetAllByTaskID(databaseID, taskID, options).Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get task log: %w", err)
	}

	log := &TaskLog{
		DatabaseID: databaseID,
		TaskID:     taskID,
		Entries:    make([]LogEntry, 0, len(stored)),
	}
	for i := len(stored) - 1; i >= 0; i-- {
		s := stored[i]
		if s.EntryID == options.AfterEntryID {
			// This range should be behave as if its exclusive, however we need
			// to perform an inclusive get so that we're still able to return
			// the last entry ID when there are no entries after AfterEntryID.
			// Skipping this entry produces the expected behavior.
			continue
		}
		log.Entries = append(log.Entries, LogEntry{
			Timestamp: s.Timestamp,
			Message:   s.Message,
			Fields:    s.Fields,
		})
	}
	if len(stored) > 0 {
		log.LastEntryID = stored[0].EntryID
	}

	return log, nil
}

func (s *Service) DeleteTaskLogs(ctx context.Context, databaseID string, taskID uuid.UUID) error {
	_, err := s.Store.TaskLogMessage.DeleteByTaskID(databaseID, taskID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete task logs: %w", err)
	}
	return nil
}

func (s *Service) DeleteAllTaskLogs(ctx context.Context, databaseID string) error {
	_, err := s.Store.TaskLogMessage.DeleteByDatabaseID(databaseID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete task logs: %w", err)
	}
	return nil
}

func (s *Service) getTasks(ctx context.Context, databaseID string, options TaskListOptions) ([]*Task, error) {
	stored, err := s.Store.Task.GetAllByDatabaseID(databaseID, options).Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return []*Task{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get tasks: %w", err)
	}
	tasks := make([]*Task, 0, len(stored))
	for _, st := range stored {
		if st != nil && st.Task != nil {
			tasks = append(tasks, st.Task)
		}
	}
	return tasks, nil
}

func (s *Service) getTasksFiltered(ctx context.Context, databaseID string, options TaskListOptions) ([]*Task, error) {
	perPage := perPageFor(options)
	tasks := make([]*Task, 0)
	if options.Limit > 0 {
		tasks = make([]*Task, 0, options.Limit)
	}

	after := options.AfterTaskID
	for {
		pageOpts := options
		pageOpts.Limit = perPage
		pageOpts.AfterTaskID = after

		stored, err := s.Store.Task.GetAllByDatabaseID(databaseID, pageOpts).Exec(ctx)
		if errors.Is(err, storage.ErrNotFound) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("failed to list tasks (paged): %w", err)
		}
		if len(stored) == 0 {
			break
		}

		for _, st := range stored {
			t := st.Task
			if !matchesFilters(t, options) {
				continue
			}
			tasks = append(tasks, t)
			if options.Limit > 0 && len(tasks) >= options.Limit {
				if len(tasks) > options.Limit {
					tasks = tasks[:options.Limit]
				}
				return tasks, nil
			}
		}

		last := stored[len(stored)-1]
		if last == nil || last.Task == nil {
			break
		}
		after = last.Task.TaskID

		if len(stored) < perPage {
			break
		}
	}

	return tasks, nil
}

func perPageFor(options TaskListOptions) int {
	const defaultPageSize = 100
	if options.Limit > 0 && options.Limit < defaultPageSize {
		return options.Limit
	}
	if options.Limit > 0 {
		return options.Limit
	}
	return defaultPageSize
}

func matchesFilters(task *Task, opts TaskListOptions) bool {
	if opts.Type != "" && task.Type != opts.Type {
		return false
	}
	if !slices.Contains(opts.Statuses, task.Status) {
		return false
	}
	if opts.NodeName != "" && (task == nil || task.NodeName != opts.NodeName) {
		return false
	}

	return true
}
