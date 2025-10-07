package workflows

import (
	"errors"

	"github.com/cschleiden/go-workflows/worker"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type Workflows struct {
	Config     config.Config
	Activities *activities.Activities
}

func (w *Workflows) Register(work *worker.Worker) error {
	if err := w.Activities.Register(work); err != nil {
		return err
	}
	errs := []error{
		work.RegisterWorkflow(w.CreatePgBackRestBackup),
		work.RegisterWorkflow(w.Failover),
		work.RegisterWorkflow(w.DeleteDatabase),
		work.RegisterWorkflow(w.PgBackRestRestore),
		work.RegisterWorkflow(w.PlanRestore),
		work.RegisterWorkflow(w.PlanUpdate),
		work.RegisterWorkflow(w.RefreshCurrentState),
		work.RegisterWorkflow(w.RestartInstance),
		work.RegisterWorkflow(w.StartInstance),
		work.RegisterWorkflow(w.StopInstance),
		work.RegisterWorkflow(w.Switchover),
		work.RegisterWorkflow(w.UpdateDatabase),
		work.RegisterWorkflow(w.ValidateSpec),
	}
	return errors.Join(errs...)
}
