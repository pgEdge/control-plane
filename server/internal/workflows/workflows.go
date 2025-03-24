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
		work.RegisterWorkflow(w.DeleteDatabase),
		work.RegisterWorkflow(w.GetDesiredState),
		work.RegisterWorkflow(w.Plan),
		work.RegisterWorkflow(w.ReconcileState),
		work.RegisterWorkflow(w.UpdateDatabase),
	}
	return errors.Join(errs...)
}
