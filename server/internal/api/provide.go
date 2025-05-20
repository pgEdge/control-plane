package api

import (
	"fmt"

	"github.com/cschleiden/go-workflows/client"
	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows"
)

func Provide(i *do.Injector) {
	providePreInitService(i)
	provideService(i)
	provideServer(i)
}

func provideServer(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Server, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get config: %w", err)
		}
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get logger: %w", err)
		}
		return NewServer(cfg, logger), nil
	})
}

func providePreInitService(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*PreInitService, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get config: %w", err)
		}
		etcdClient, err := do.Invoke[*etcd.EmbeddedEtcd](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get embedded etcd: %w", err)
		}
		return NewPreInitService(cfg, etcdClient), nil
	})
}

func provideService(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Service, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get config: %w", err)
		}
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get logger: %w", err)
		}
		etcdClient, err := do.Invoke[*etcd.EmbeddedEtcd](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get embedded etcd: %w", err)
		}
		hostSvc, err := do.Invoke[*host.Service](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get host service: %w", err)
		}
		dbSvc, err := do.Invoke[*database.Service](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get database service: %w", err)
		}
		wfClient, err := do.Invoke[*client.Client](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get workflow client: %w", err)
		}
		taskSvc, err := do.Invoke[*task.Service](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get task service: %w", err)
		}
		workflows, err := do.Invoke[*workflows.Workflows](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get workflows: %w", err)
		}

		return NewService(cfg, logger, etcdClient, hostSvc, dbSvc, taskSvc, wfClient, workflows), nil
	})
}
