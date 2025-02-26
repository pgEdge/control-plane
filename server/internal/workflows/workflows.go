package workflows

import (
	"errors"

	"github.com/cschleiden/go-workflows/worker"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities/general"
	"github.com/pgEdge/control-plane/server/internal/workflows/swarm"
)

type Workflows struct {
	Config            config.Config
	GeneralActivities *general.Activities
	SwarmWorkflows    *swarm.Workflows
}

func (w *Workflows) Register(work *worker.Worker) error {
	errs := []error{
		work.RegisterWorkflow(w.CreateDatabase),
		w.SwarmWorkflows.Register(work),
	}
	return errors.Join(errs...)
}
