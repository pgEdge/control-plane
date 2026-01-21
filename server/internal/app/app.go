package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	"google.golang.org/grpc/grpclog"

	"github.com/pgEdge/control-plane/server/internal/api"
	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/migrate"
	"github.com/pgEdge/control-plane/server/internal/monitor"
	"github.com/pgEdge/control-plane/server/internal/scheduler"
	"github.com/pgEdge/control-plane/server/internal/workflows"
)

type ErrorProducer interface {
	Error() <-chan error
}

type Orchestrator interface {
	host.Orchestrator
	database.Orchestrator
}

type App struct {
	i                *do.Injector
	cfg              config.Config
	logger           zerolog.Logger
	etcd             etcd.Etcd
	api              *api.Server
	errCh            chan error
	serviceCtx       context.Context
	serviceCtxCancel context.CancelFunc
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
		errCh:  make(chan error, 1),
	}

	return app, nil
}

func (a *App) Run(parentCtx context.Context) error {
	// The caller of this method cancels parentCtx to trigger a shutdown.
	// However, some shut down procedures need an active context. We provide
	// this separate context to services that are managed by `App` and we cancel
	// it after all the shutdown processes have finished.
	a.serviceCtx, a.serviceCtxCancel = context.WithCancel(context.Background())

	// grpclog needs to be configured before grpc.Dial is called
	grpcLogger, err := do.Invoke[grpclog.LoggerV2](a.i)
	if err != nil {
		return fmt.Errorf("failed to initialize grpc logger: %w", err)
	}
	grpclog.SetLoggerV2(grpcLogger)

	e, err := do.Invoke[etcd.Etcd](a.i)
	if err != nil {
		return fmt.Errorf("failed to initialize etcd: %w", err)
	}

	apiServer, err := do.Invoke[*api.Server](a.i)
	if err != nil {
		return fmt.Errorf("failed to initialize api server: %w", err)
	}
	a.addErrorProducer(parentCtx, apiServer)

	a.etcd = e
	a.api = apiServer

	initialized, err := a.etcd.IsInitialized()
	if err != nil {
		return fmt.Errorf("failed to check if etcd is initialized: %w", err)
	}
	if initialized {
		if err := a.etcd.Start(a.serviceCtx); err != nil {
			return fmt.Errorf("failed to start etcd: %w", err)
		}
		a.addErrorProducer(parentCtx, a.etcd)

		return a.shutdown(a.runInitialized(parentCtx))
	} else {
		return a.shutdown(a.runPreInitialization(parentCtx))
	}
}

func (a *App) addErrorProducer(parentCtx context.Context, producer ErrorProducer) {
	go func() {
		select {
		case <-parentCtx.Done():
			return
		case err := <-producer.Error():
			if err != nil {
				a.errCh <- err
			}
		}
	}()
}

func (a *App) runPreInitialization(parentCtx context.Context) error {
	if err := a.api.ServePreInit(a.serviceCtx); err != nil {
		return fmt.Errorf("failed to serve pre-init API: %w", err)
	}

	a.logger.Info().
		Str("state", "uninitialized").
		Msg("server ready")

	select {
	case <-parentCtx.Done():
		a.logger.Info().Msg("got shutdown signal")
		return nil
	case err := <-a.errCh:
		return err
	case <-a.etcd.Initialized():
		a.logger.Info().Msg("etcd initialized")
		a.addErrorProducer(parentCtx, a.etcd)
		config.UpdateInjectedConfig(a.i)
		return a.runInitialized(parentCtx)
	}
}

func (a *App) runInitialized(parentCtx context.Context) error {
	handleError := func(err error) error {
		a.api.HandleInitializationError(err)
		return err
	}

	// Run migrations before starting other services
	migrationRunner, err := do.Invoke[*migrate.Runner](a.i)
	if err != nil {
		return handleError(fmt.Errorf("failed to initialize migration runner: %w", err))
	}
	if err := migrationRunner.Run(a.serviceCtx); err != nil {
		return handleError(fmt.Errorf("failed to run migrations: %w", err))
	}

	certSvc, err := do.Invoke[*certificates.Service](a.i)
	if err != nil {
		return handleError(fmt.Errorf("failed to initialize certificate service: %w", err))
	}
	if err := certSvc.Start(a.serviceCtx); err != nil {
		return handleError(fmt.Errorf("failed to start certificate service: %w", err))
	}

	hostSvc, err := do.Invoke[*host.Service](a.i)
	if err != nil {
		return handleError(fmt.Errorf("failed to initialize host service: %w", err))
	}
	if err := hostSvc.UpdateHost(a.serviceCtx); err != nil {
		return handleError(fmt.Errorf("failed to update host: %w", err))
	}

	hostTicker, err := do.Invoke[*host.UpdateTicker](a.i)
	if err != nil {
		return handleError(fmt.Errorf("failed to initialize host ticker: %w", err))
	}
	hostTicker.Start(a.serviceCtx)

	monitorSvc, err := do.Invoke[*monitor.Service](a.i)
	if err != nil {
		return handleError(fmt.Errorf("failed to initialize monitor service: %w", err))
	}
	if err := monitorSvc.Start(a.serviceCtx); err != nil {
		return handleError(fmt.Errorf("failed to start monitor service: %w", err))
	}

	schedulerSvc, err := do.Invoke[*scheduler.Service](a.i)
	if err != nil {
		return handleError(fmt.Errorf("failed to initialize scheduler service: %w", err))
	}
	a.addErrorProducer(parentCtx, schedulerSvc)
	if err := schedulerSvc.Start(a.serviceCtx); err != nil {
		return handleError(fmt.Errorf("failed to start scheduler service: %w", err))
	}

	worker, err := do.Invoke[*workflows.Worker](a.i)
	if err != nil {
		return handleError(fmt.Errorf("failed to initialize worker: %w", err))
	}
	if err := worker.Start(a.serviceCtx); err != nil {
		return handleError(fmt.Errorf("failed to start worker: %w", err))
	}

	if err := a.api.ServePostInit(a.serviceCtx); err != nil {
		return handleError(fmt.Errorf("failed to serve post-init API: %w", err))
	}

	a.logger.Info().
		Str("state", "initialized").
		Msg("server ready")

	select {
	case <-parentCtx.Done():
		a.logger.Info().Msg("got shutdown signal")
		return nil
	case err := <-a.errCh:
		return err
	}
}

func (a *App) shutdown(reason error) error {
	defer a.logger.Info().Msg("shutdown complete")
	defer a.serviceCtxCancel()

	if reason != nil {
		a.logger.Err(reason).Msg("shutting down due to error")
	}

	a.logger.Info().
		Int64("stop_grace_period_seconds", a.cfg.StopGracePeriodSeconds).
		Msg("attempting to gracefully shut down")

	errCh := make(chan error, 1)

	go func() {
		errCh <- a.i.Shutdown()
	}()

	var errs = []error{reason}

	gracePeriod := time.Duration(a.cfg.StopGracePeriodSeconds) * time.Second

	select {
	case err := <-errCh:
		errs = append(errs, err)
	case <-time.After(gracePeriod):
		errs = append(errs, fmt.Errorf("graceful shutdown timed out after %d seconds", a.cfg.StopGracePeriodSeconds))
	}

	return errors.Join(errs...)
}
