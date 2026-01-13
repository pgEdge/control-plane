package task

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/utils"
)

type TaskLogWriter struct {
	writer *utils.LineWriter
}

func NewTaskLogWriter(ctx context.Context, service *Service, scope Scope, entityID string, taskID uuid.UUID) *TaskLogWriter {
	return &TaskLogWriter{
		writer: utils.NewLineWriter(func(b []byte) error {
			err := service.AddLogEntry(ctx, scope, entityID, taskID, LogEntry{
				Message: utils.Clean(string(b)), // remove all control characters
			})
			if err != nil {
				return fmt.Errorf("failed to add log entry: %w", err)
			}
			return nil
		}),
	}
}

func (w *TaskLogWriter) Write(p []byte) (n int, err error) {
	n, err = w.writer.Write(p)
	if err != nil {
		return n, fmt.Errorf("failed to write log entry: %w", err)
	}
	return n, nil
}

func (w *TaskLogWriter) Close() error {
	return w.writer.Close()
}
