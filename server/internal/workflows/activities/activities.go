package activities

import (
	"errors"

	"github.com/cschleiden/go-workflows/worker"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/task"
)

type Activities struct {
	Config          config.Config
	Injector        *do.Injector
	Orchestrator    database.Orchestrator
	DatabaseService *database.Service
	TaskSvc         *task.Service
}

func (a *Activities) Register(work *worker.Worker) error {
	errs := []error{
		work.RegisterActivity(a.ApplyEvent),
		work.RegisterActivity(a.CancelSwitchover),
		work.RegisterActivity(a.CheckClusterHealth),
		work.RegisterActivity(a.CleanupInstance),
		work.RegisterActivity(a.CreatePgBackRestBackup),
		work.RegisterActivity(a.DeleteDbEntities),
		work.RegisterActivity(a.GenerateServiceInstanceResources),
		work.RegisterActivity(a.GetCurrentState),
		work.RegisterActivity(a.GetInstanceResources),
		work.RegisterActivity(a.GetPrimaryInstance),
		work.RegisterActivity(a.GetRestoreResources),
		work.RegisterActivity(a.LogTaskEvent),
		work.RegisterActivity(a.PerformFailover),
		work.RegisterActivity(a.PerformSwitchover),
		work.RegisterActivity(a.PersistPlanSummaries),
		work.RegisterActivity(a.PersistState),
		work.RegisterActivity(a.PlanRefresh),
		work.RegisterActivity(a.RemoveHost),
		work.RegisterActivity(a.RestartInstance),
		work.RegisterActivity(a.SelectCandidate),
		work.RegisterActivity(a.StartInstance),
		work.RegisterActivity(a.StopInstance),
		work.RegisterActivity(a.UpdateDbState),
		work.RegisterActivity(a.UpdateTask),
		work.RegisterActivity(a.ValidateInstanceSpecs),
	}
	return errors.Join(errs...)
}
