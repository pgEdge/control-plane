package workflows

import (
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

func (w *Workflows) Register(work *worker.Worker) {
	work.RegisterWorkflow(w.CreateDatabase)
	w.SwarmWorkflows.Register(work)
	// work.RegisterActivity(w.General.CreateDataDir)
	// work.RegisterActivity(w.General.CreateLoopDevice)
	// work.RegisterActivity(w.General.CreateEtcdUser)
	// work.RegisterActivity(w.Swarm.CreateBridgeConfig)
	// work.RegisterActivity(w.Swarm.CreateDBNetworks)
	// work.RegisterActivity(w.Swarm.CreateDBService)
	// work.RegisterWorkflow(w.CreateInstance)
	// work.RegisterWorkflow(w.CreateHostInstances)
	// work.RegisterWorkflow(w.CreateCohortInstances)
}
