package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/api"
	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/workflows"
)

type Orchestrator interface {
	host.Orchestrator
	database.Orchestrator
}

type App struct {
	i      *do.Injector
	cfg    config.Config
	logger zerolog.Logger
	etcd   *etcd.EmbeddedEtcd
	api    *api.Server
}

func NewApp(i *do.Injector) (*App, error) {
	cfg, err := do.Invoke[config.Config](i)
	if err != nil {
		return nil, err
	}
	logger, err := do.Invoke[zerolog.Logger](i)
	if err != nil {
		return nil, err
	}

	app := &App{
		i:      i,
		cfg:    cfg,
		logger: logger,
	}

	return app, nil
}

func (a *App) Run(ctx context.Context) error {
	embeddedEtcd, err := do.Invoke[*etcd.EmbeddedEtcd](a.i)
	if err != nil {
		return fmt.Errorf("failed to initialize etcd: %w", err)
	}
	apiServer, err := do.Invoke[*api.Server](a.i)
	if err != nil {
		return fmt.Errorf("failed to initialize api server: %w", err)
	}

	a.etcd = embeddedEtcd
	a.api = apiServer

	initialized, err := a.etcd.IsInitialized()
	if err != nil {
		return fmt.Errorf("failed to check if etcd is initialized: %w", err)
	}
	if initialized {
		if err := a.etcd.Start(ctx); err != nil {
			return fmt.Errorf("failed to start etcd: %w", err)
		}
		return a.runInitialized(ctx)
	} else {
		return a.runPreInitialization(ctx)
	}
}

func (a *App) runPreInitialization(ctx context.Context) error {
	svc, err := do.Invoke[*api.PreInitService](a.i)
	if err != nil {
		return fmt.Errorf("failed to initialize pre-init api service: %w", err)
	}

	a.api.Serve(ctx, svc)

	select {
	case <-ctx.Done():
		a.logger.Info().Msg("got shutdown signal")
		return a.Shutdown(nil)
	case err := <-a.api.Error():
		return a.Shutdown(err)
	case <-a.etcd.Initialized():
		a.logger.Info().Msg("etcd initialized")
		return a.runInitialized(ctx)
	}
}

func (a *App) runInitialized(ctx context.Context) error {
	svc, err := do.Invoke[*api.Service](a.i)
	if err != nil {
		return fmt.Errorf("failed to initialize api service: %w", err)
	}
	a.api.Serve(ctx, svc)

	certSvc, err := do.Invoke[*certificates.Service](a.i)
	if err != nil {
		return fmt.Errorf("failed to initialize certificate service: %w", err)
	}
	if err := certSvc.Start(ctx); err != nil {
		return fmt.Errorf("failed to start certificate service: %w", err)
	}

	hostSvc, err := do.Invoke[*host.Service](a.i)
	if err != nil {
		return fmt.Errorf("failed to initialize host service: %w", err)
	}
	if err := hostSvc.UpdateHost(ctx); err != nil {
		return fmt.Errorf("failed to update host: %w", err)
	}

	hostTicker, err := do.Invoke[*host.UpdateTicker](a.i)
	if err != nil {
		return fmt.Errorf("failed to initialize host ticker: %w", err)
	}
	hostTicker.Start(ctx)

	worker, err := do.Invoke[*workflows.Worker](a.i)
	if err != nil {
		return fmt.Errorf("failed to initialize worker: %w", err)
	}
	if err := worker.Start(ctx); err != nil {
		return fmt.Errorf("failed to start worker: %w", err)
	}

	select {
	case <-ctx.Done():
		a.logger.Info().Msg("got shutdown signal")
		return a.Shutdown(nil)
	case err := <-a.api.Error():
		return a.Shutdown(err)
	}
}

func (a *App) Shutdown(reason error) error {
	a.logger.Info().Msg("attempting to gracefully shut down")

	errs := []error{
		reason,
		a.i.Shutdown(),
	}

	return errors.Join(errs...)
}
