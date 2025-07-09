package scheduler

import (
	"context"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
)

const ResourceTypeScheduledJob resource.Type = "scheduler.job"

func ScheduledJobResourceIdentifier(hostID string) resource.Identifier {
	return resource.Identifier{
		ID:   hostID,
		Type: ResourceTypeScheduledJob,
	}
}

type ScheduledJobResource struct {
	ID        string                 `json:"id"`                   // Unique job identifier
	CronExpr  string                 `json:"cron_expr"`            // Cron expression for scheduling
	Workflow  string                 `json:"workflow"`             // Name of the workflow to execute
	Args      map[string]interface{} `json:"args"`                 // Arguments to the workflow
	DependsOn []resource.Identifier  `json:"depends_on,omitempty"` // Optional resource dependencies
	HostID    string                 `json:"host_id,omitempty"`    // Host to execute the job on
}

func NewScheduledJobResource(
	id, cronExpr, workflow, hostID string,
	args map[string]interface{},
	dependsOn []resource.Identifier,
) *ScheduledJobResource {
	return &ScheduledJobResource{
		ID:        id,
		CronExpr:  cronExpr,
		Workflow:  workflow,
		Args:      args,
		HostID:    hostID,
		DependsOn: dependsOn,
	}
}

func (r *ScheduledJobResource) ResourceVersion() string {
	return "1"
}
func (r *ScheduledJobResource) DiffIgnore() []string {
	return nil
}

func (r *ScheduledJobResource) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeHost,
		ID:   r.HostID,
	}
}

func (r *ScheduledJobResource) Identifier() resource.Identifier {
	return ScheduledJobResourceIdentifier(r.HostID)
}
func (r *ScheduledJobResource) Dependencies() []resource.Identifier {
	return r.DependsOn
}
func (r *ScheduledJobResource) Refresh(ctx context.Context, rc *resource.Context) error {
	service, err := do.Invoke[*Service](rc.Injector)
	if err != nil {
		return err
	}

	if !service.ExitsJob(r.ID) {
		return resource.ErrNotFound
	}
	return nil
}

func (r *ScheduledJobResource) Create(ctx context.Context, rc *resource.Context) error {
	service, err := do.Invoke[*Service](rc.Injector)
	if err != nil {
		return err
	}
	job := &StoredScheduledJob{
		ID:       r.ID,
		CronExpr: r.CronExpr,
		Workflow: r.Workflow,
		ArgsJSON: r.Args,
	}

	if err := service.RegisterJob(ctx, job); err != nil {
		return fmt.Errorf("failed to register scheduled job: %w", err)
	}
	return nil
}

func (r *ScheduledJobResource) Delete(ctx context.Context, rc *resource.Context) error {
	service, err := do.Invoke[*Service](rc.Injector)
	if err != nil {
		return err
	}

	if err := service.DeleteJob(ctx, r.ID); err != nil {
		return fmt.Errorf("failed to delete scheduled job: %w", err)
	}
	return nil
}

func (r *ScheduledJobResource) Update(ctx context.Context, rc *resource.Context) error {
	if err := r.Delete(ctx, rc); err != nil {
		return err
	}
	return r.Create(ctx, rc)
}
