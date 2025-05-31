package task

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type Type string

func (t Type) String() string {
	return string(t)
}

const (
	TypeCreate      Type = "create"
	TypeUpdate      Type = "update"
	TypeDelete      Type = "delete"
	TypeNodeBackup  Type = "node_backup"
	TypeRestore     Type = "restore"
	TypeNodeRestore Type = "node_restore"
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

var completedStatuses = ds.NewSet(
	StatusCompleted,
	StatusFailed,
	StatusCanceled,
)

type Task struct {
	ParentID            uuid.UUID `json:"parent_id"`
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

func (t *Task) IsComplete() bool {
	return completedStatuses.Has(t.Status)
}

type Options struct {
	ParentID            uuid.UUID `json:"parent_id"`
	DatabaseID          uuid.UUID `json:"database_id"`
	NodeName            string    `json:"node_name"`
	InstanceID          uuid.UUID `json:"instance_id"`
	HostID              uuid.UUID `json:"host_id"`
	Type                Type      `json:"type"`
	WorkflowInstanceID  string    `json:"workflow_id"`
	WorkflowExecutionID string    `json:"workflow_execution_id"`
}

func (o Options) validate() error {
	if o.DatabaseID == uuid.Nil {
		return errors.New("database ID is required when creating a new task")
	}
	if o.Type == "" {
		return errors.New("type is required when creating a new task")
	}
	return nil
}

func NewTask(opts Options) (*Task, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	taskID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("failed to generate task ID: %w", err)
	}

	return &Task{
		ParentID:            opts.ParentID,
		DatabaseID:          opts.DatabaseID,
		NodeName:            opts.NodeName,
		InstanceID:          opts.InstanceID,
		HostID:              opts.HostID,
		WorkflowExecutionID: opts.WorkflowExecutionID,
		WorkflowInstanceID:  opts.WorkflowInstanceID,
		TaskID:              taskID,
		CreatedAt:           time.Now(),
		Type:                opts.Type,
		Status:              StatusPending,
	}, nil
}

type UpdateOptions struct {
	NodeName            *string    `json:"node_name,omitempty"`
	InstanceID          *uuid.UUID `json:"instance_id,omitempty"`
	HostID              *uuid.UUID `json:"host_id,omitempty"`
	WorkflowInstanceID  *string    `json:"workflow_instance_id,omitempty"`
	WorkflowExecutionID *string    `json:"workflow_execution_id,omitempty"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
	Status              *Status    `json:"status,omitempty"`
	Error               *string    `json:"error,omitempty"`
}

func UpdateStart() UpdateOptions {
	return UpdateOptions{
		Status: utils.PointerTo(StatusRunning),
	}
}

func UpdateComplete() UpdateOptions {
	return UpdateOptions{
		Status:      utils.PointerTo(StatusCompleted),
		CompletedAt: utils.PointerTo(time.Now()),
	}
}

func UpdateFail(cause error) UpdateOptions {
	return UpdateOptions{
		Status:      utils.PointerTo(StatusFailed),
		CompletedAt: utils.PointerTo(time.Now()),
		Error:       utils.PointerTo(cause.Error()),
	}
}

func (t *Task) Update(options UpdateOptions) {
	if options.NodeName != nil {
		t.NodeName = *options.NodeName
	}
	if options.InstanceID != nil {
		t.InstanceID = *options.InstanceID
	}
	if options.HostID != nil {
		t.HostID = *options.HostID
	}
	if options.WorkflowInstanceID != nil {
		t.WorkflowInstanceID = *options.WorkflowInstanceID
	}
	if options.WorkflowExecutionID != nil {
		t.WorkflowExecutionID = *options.WorkflowExecutionID
	}
	if !t.IsComplete() {
		// These fields should only get set once
		if options.CompletedAt != nil {
			t.CompletedAt = *options.CompletedAt
		}
		if options.Status != nil {
			t.Status = *options.Status
		}
		if options.Error != nil {
			t.Error = *options.Error
		}
	}
}

func (t *Task) Start() {
	t.Update(UpdateStart())
}

func (t *Task) SetFailed(cause error) {
	t.Update(UpdateFail(cause))
}

func (t *Task) SetCompleted() {
	t.Update(UpdateComplete())
}

type TaskLog struct {
	DatabaseID  uuid.UUID  `json:"database_id"`
	TaskID      uuid.UUID  `json:"id"`
	LastEntryID uuid.UUID  `json:"last_entry_id"`
	Entries     []LogEntry `json:"entries"`
}
