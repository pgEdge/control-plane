package workflows

import (
	"github.com/cschleiden/go-workflows/backend"
	"github.com/cschleiden/go-workflows/client"
	"github.com/rs/zerolog"
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
	"github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd"
)

func Provide(i *do.Injector) {
	provideStore(i)
	provideBackend(i)
	provideClient(i)
	provideWorkflows(i)
	provideWorker(i)
	provideService(i)
}

func provideWorker(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Worker, error) {
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, err
		}
		be, err := do.Invoke[backend.Backend](i)
		if err != nil {
			return nil, err
		}
		workflows, err := do.Invoke[*Workflows](i)
		if err != nil {
			return nil, err
		}
		orch, err := do.Invoke[Orchestrator](i)
		if err != nil {
			return nil, err
		}
		return NewWorker(logger, be, workflows, orch)
	})
}

func provideWorkflows(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Workflows, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		activities, err := do.Invoke[*activities.Activities](i)
		if err != nil {
			return nil, err
		}

		return &Workflows{
			Config:     cfg,
			Activities: activities,
		}, nil
	})
}

func provideClient(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*client.Client, error) {
		be, err := do.Invoke[backend.Backend](i)
		if err != nil {
			return nil, err
		}
		return client.New(be), nil
	})
}

func provideBackend(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (backend.Backend, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		store, err := do.Invoke[*etcd.Store](i)
		if err != nil {
			return nil, err
		}
		zl, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, err
		}
		logger := logging.Slog(zl, zl.GetLevel())
		backendOpts := backend.ApplyOptions(
			backend.WithLogger(logger),
		)
		return etcd.NewBackend(store, backendOpts, cfg.HostID), nil
	})
}

func provideStore(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*etcd.Store, error) {
		client, err := do.Invoke[*clientv3.Client](i)
		if err != nil {
			return nil, err
		}
		config, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		return etcd.NewStore(client, config.EtcdKeyRoot), nil
	})
}

func provideService(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Service, error) {
		config, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		client, err := do.Invoke[*client.Client](i)
		if err != nil {
			return nil, err
		}
		workflows, err := do.Invoke[*Workflows](i)
		if err != nil {
			return nil, err
		}
		taskSvc, err := do.Invoke[*task.Service](i)
		if err != nil {
			return nil, err
		}

		return NewService(config, client, taskSvc, workflows), nil
	})
}
