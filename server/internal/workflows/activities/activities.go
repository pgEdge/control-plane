package activities

import (
	"errors"

	"github.com/cschleiden/go-workflows/worker"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
)

func RegisterActivities(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Activities, error) {
		orch, err := do.Invoke[database.Orchestrator](i)
		if err != nil {
			return nil, err
		}

		return &Activities{
			Injector:     i,
			Orchestrator: orch,
		}, nil
	})
}

type Activities struct {
	Config       config.Config
	Injector     *do.Injector
	Orchestrator database.Orchestrator
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
		work.RegisterActivity(a.PersistState),
		work.RegisterActivity(a.PlanRefresh),
		work.RegisterActivity(a.Plan),
		work.RegisterActivity(a.RestoreSpec),
		work.RegisterActivity(a.UpdateDbState),
		work.RegisterActivity(a.UpdateTask),
		work.RegisterActivity(a.UpdateTaskStatus),
	}
	return errors.Join(errs...)
}
