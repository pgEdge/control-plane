package task

import (
	"time"

	"github.com/google/uuid"
)

type Type string

func (t Type) String() string {
	return string(t)
}

const (
	TypeCreate  Type = "create"
	TypeUpdate  Type = "update"
	TypeDelete  Type = "delete"
	TypeBackup  Type = "backup"
	TypeRestore Type = "restore"
)

type Status string

func (s Status) String() string {
	return string(s)
}

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
	StatusUnknown   Status = "unknown"
)

type Task struct {
	DatabaseID          uuid.UUID `json:"database_id"`
	NodeName            string    `json:"node_name"`
	InstanceID          uuid.UUID `json:"instance_id"`
	HostID              uuid.UUID `json:"host_id"`
	TaskID              uuid.UUID `json:"task_id"`
	CreatedAt           time.Time `json:"created_at"`
	CompletedAt         time.Time `json:"completed_at"`
	Type                Type      `json:"type"`
	WorkflowInstanceID  string    `json:"workflow_id"`
	WorkflowExecutionID string    `json:"workflow_execution_id"`
	Status              Status    `json:"status"`
	Error               string    `json:"error"`
}

func NewTask(databaseID uuid.UUID, taskType Type) (*Task, error) {
	taskID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	return &Task{
		DatabaseID: databaseID,
		TaskID:     taskID,
		CreatedAt:  time.Now(),
		Type:       taskType,
		Status:     StatusPending,
	}, nil
}

func (t *Task) SetFailed(err error) {
	t.CompletedAt = time.Now()
	t.Error = err.Error()
	t.Status = StatusFailed
}

func (t *Task) SetCompleted() {
	t.CompletedAt = time.Now()
	t.Status = StatusCompleted
}

type TaskLog struct {
	DatabaseID uuid.UUID `json:"database_id"`
	TaskID     uuid.UUID `json:"id"`
	LastLineID uuid.UUID `json:"last_line_id"`
	Lines      []string  `json:"lines"`
}
