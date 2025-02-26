package swarm

import (
	"github.com/cschleiden/go-workflows/worker"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities/general"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities/swarm"
)

type Workflows struct {
	General *general.Activities
	Swarm   *swarm.Activities
}

func (w *Workflows) Register(work *worker.Worker) {
	work.RegisterActivity(w.General.CreateDataDir)
	work.RegisterActivity(w.General.CreateLoopDevice)
	work.RegisterActivity(w.General.CreateEtcdUser)
	work.RegisterActivity(w.Swarm.CreateBridgeConfig)
	work.RegisterActivity(w.Swarm.CreateDBNetworks)
	work.RegisterActivity(w.Swarm.CreateDBService)
	work.RegisterActivity(w.Swarm.WriteInstanceConfigs)
	work.RegisterActivity(w.Swarm.GetServiceSpec)
	work.RegisterActivity(w.Swarm.InitializeDB)
	work.RegisterWorkflow(w.CreateInstance)
	work.RegisterWorkflow(w.CreateHostInstances)
	work.RegisterWorkflow(w.CreateCohortInstances)
}
