package scheduler

import (
	"time"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/workflows"
	"github.com/rs/zerolog"
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func Provide(i *do.Injector) {
	provideLeaderStore(i)
	provideElector(i)
	provideScheduledJobStore(i)
	provideService(i)
	provideExecutor(i)
}

func provideLeaderStore(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*LeaderStore, error) {
		client, err := do.Invoke[*clientv3.Client](i)
		if err != nil {
			return nil, err
		}
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		return NewLeaderStore(client, cfg.EtcdKeyRoot), nil
	})
}

func provideElector(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Elector, error) {
		store, err := do.Invoke[*LeaderStore](i)
		if err != nil {
			return nil, err
		}
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, err
		}
		return NewElector(cfg.HostID, store, logger, 30*time.Second), nil
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
