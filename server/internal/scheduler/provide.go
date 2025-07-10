package scheduler

import (
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/workflows"
	"github.com/rs/zerolog"
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func Provide(i *do.Injector) {
	provideStore(i)
	provideService(i)
	provideExecutor(i)
}

func provideStore(i *do.Injector) {
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
		return NewService(logger, store, executor, client), nil
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
