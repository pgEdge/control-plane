package scheduler

import (
	"time"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

const (
	ScheduledJobPrefix = "scheduled_jobs"

	JobStatusPending   = "pending"
	JobStatusRunning   = "running"
	JobStatusCompleted = "completed"
	JobStatusFailed    = "failed"

	WorkflowCreatePgBackRestBackup = "CreatePgBackRestBackup"
)

type StoredScheduledJob struct {
	storage.StoredValue
	ID       string         `json:"id"`
	CronExpr string         `json:"cron_expr"`
	Workflow string         `json:"workflow"`
	ArgsJSON map[string]any `json:"args_json"`
	LastRun  *time.Time     `json:"last_run,omitempty"`
	NextRun  *time.Time     `json:"next_run,omitempty"`
	Status   string         `json:"status,omitempty"`
	Logs     string         `json:"logs,omitempty"`
}
