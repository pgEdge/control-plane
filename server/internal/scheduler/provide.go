package scheduler

import (
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/election"
	"github.com/pgEdge/control-plane/server/internal/workflows"
)

const electionName election.Name = "scheduler"
const electionTTL time.Duration = 30 * time.Second

func Provide(i *do.Injector) {
	provideElector(i)
	provideScheduledJobStore(i)
	provideService(i)
	provideExecutor(i)
}

func provideElector(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Elector, error) {
		electionSvc, err := do.Invoke[*election.Service](i)
		if err != nil {
			return nil, err
		}
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}

		candidate := electionSvc.NewCandidate(electionName, cfg.HostID, electionTTL)
		return NewElector(candidate), nil
	})
}

func provideScheduledJobStore(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*ScheduledJobStore, error) {
		client, err := do.Invoke[*clientv3.Client](i)
		if err != nil {
			return nil, err
		}
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		return NewScheduledJobStore(client, cfg.EtcdKeyRoot), nil
	})
}

func provideService(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Service, error) {
		store, err := do.Invoke[*ScheduledJobStore](i)
		if err != nil {
			return nil, err
		}
		executor, err := do.Invoke[WorkflowExecutor](i)
		if err != nil {
			return nil, err
		}
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, err
		}
		client, err := do.Invoke[*clientv3.Client](i)
		if err != nil {
			return nil, err
		}
		elector, err := do.Invoke[*Elector](i)
		if err != nil {
			return nil, err
		}
		return NewService(logger, store, executor, client, elector), nil
	})
}

func provideExecutor(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (WorkflowExecutor, error) {
		workflowSvc, err := do.Invoke[*workflows.Service](i)
		if err != nil {
			return nil, err
		}
		dbSvc, err := do.Invoke[*database.Service](i)
		if err != nil {
			return nil, err
		}
		return NewDefaultWorkflowExecutor(workflowSvc, dbSvc), nil
	})
}
