package scheduler

import (
	"github.com/pgEdge/control-plane/server/internal/storage"
)

const (
	ScheduledJobPrefix    = "scheduled_jobs"
	SchedulerLeaderPrefix = "scheduler-leader"

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
}
