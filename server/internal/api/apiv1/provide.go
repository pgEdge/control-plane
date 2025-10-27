package apiv1

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/cluster"
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
	providePreInitHandlers(i)
	providePostInitHandlers(i)
	provideService(i)
}

func providePreInitHandlers(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*PreInitHandlers, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get config: %w", err)
		}
		e, err := do.Invoke[etcd.Etcd](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get embedded etcd: %w", err)
		}
		return NewPreInitHandlers(cfg, e), nil
	})
}

func providePostInitHandlers(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*PostInitHandlers, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get config: %w", err)
		}
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get logger: %w", err)
		}
		e, err := do.Invoke[etcd.Etcd](i)
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
		taskSvc, err := do.Invoke[*task.Service](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get task service: %w", err)
		}
		workflowSvc, err := do.Invoke[*workflows.Service](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get workflow service: %w", err)
		}
		clusterSvc, err := do.Invoke[*cluster.Service](i)
		if err != nil {
			return nil, fmt.Errorf("failed to get cluster service: %w", err)
		}

		return NewPostInitHandlers(cfg, logger, e, hostSvc, dbSvc, taskSvc, workflowSvc, clusterSvc), nil
	})
}

func provideService(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Service, error) {
		return NewService(i), nil
	})
}
