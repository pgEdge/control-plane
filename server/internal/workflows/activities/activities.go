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
	Config       config.Config
	Injector     *do.Injector
	Orchestrator database.Orchestrator
	TaskSvc      *task.Service
}

func (a *Activities) Register(work *worker.Worker) error {
	errs := []error{
		work.RegisterActivity(a.ApplyEvent),
		work.RegisterActivity(a.CreatePgBackRestBackup),
		work.RegisterActivity(a.DeleteDbEntities),
		work.RegisterActivity(a.GetCurrentState),
		work.RegisterActivity(a.GetInstanceResources),
		work.RegisterActivity(a.GetPrimaryInstance),
		work.RegisterActivity(a.GetRestoreResources),
		work.RegisterActivity(a.LogTaskEvent),
		work.RegisterActivity(a.PersistState),
		work.RegisterActivity(a.Plan),
		work.RegisterActivity(a.PlanRefresh),
		work.RegisterActivity(a.RestoreSpec),
		work.RegisterActivity(a.UpdateDbState),
		work.RegisterActivity(a.UpdateTask),
		work.RegisterActivity(a.ValidateInstanceSpec),
	}
	return errors.Join(errs...)
}
