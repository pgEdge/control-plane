package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/cschleiden/go-workflows/backend"
	"github.com/cschleiden/go-workflows/client"
	"github.com/rs/zerolog"
	slogzerolog "github.com/samber/slog-zerolog/v2"
	"github.com/spf13/afero"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/api"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/exec"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/ipam"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/swarm"
	etcd_backend "github.com/pgEdge/control-plane/server/internal/workflows/backend/etcd"
	"github.com/pgEdge/control-plane/server/internal/workflows/worker"
	// "github.com/pgEdge/control-plane/server/internal/host/swarm"
)

type Orchestrator interface {
	host.Orchestrator
	database.Orchestrator
}

type App struct {
	cfg        config.Config
	logger     zerolog.Logger
	etcd       *etcd.EmbeddedEtcd
	client     *clientv3.Client
	apiSvc     *api.DynamicService
	api        *api.Server
	hostTicker *host.UpdateTicker
	docker     *docker.Docker
}

func NewApp(
	cfg config.Config,
	logger zerolog.Logger,
) *App {
	app := &App{
		cfg:    cfg,
		logger: logger,
	}

	return app
}

func (a *App) Run(ctx context.Context) error {
	a.etcd = etcd.NewEmbeddedEtcd(a.cfg, a.logger)
	a.apiSvc = api.NewDynamicService()
	a.api = api.NewServer(a.cfg, a.logger, a.apiSvc)

	initialized, err := a.etcd.IsInitialized()
	if err != nil {
		return fmt.Errorf("failed to check if etcd is initialized: %w", err)
	}
	if initialized {
		if err := a.etcd.Start(ctx); err != nil {
			return fmt.Errorf("failed to start etcd: %w", err)
		}
		a.api.Start(ctx)
		return a.runInitialized(ctx)
	} else {
		return a.runPreInitialization(ctx)
	}
}

func (a *App) runPreInitialization(ctx context.Context) error {
	svc := api.NewPreInitService(a.cfg, a.etcd)
	a.apiSvc.UpdateImpl(svc)

	a.api.Start(ctx)

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
	var orchestrator Orchestrator
	switch a.cfg.Orchestrator {
	case config.OrchestratorSwarm:
		d, err := docker.NewDocker()
		if err != nil {
			return fmt.Errorf("failed to create docker client: %w", err)
		}
		a.docker = d
		orchestrator = swarm.NewOrchestrator(a.cfg, d)
	}

	etcdClient, err := a.etcd.GetClient()
	if err != nil {
		return fmt.Errorf("failed to get etcd client: %w", err)
	}
	a.client = etcdClient

	storeRoot := a.cfg.ClusterID.String()

	workflowsStore := etcd_backend.NewStore(a.client, storeRoot)
	workflowLevel := slog.LevelDebug
	for sl, zl := range slogzerolog.LogLevels {
		if zl == a.logger.GetLevel() {
			workflowLevel = sl
			break
		}
	}
	workflowLogger := a.logger.With().
		CallerWithSkipFrameCount(3).
		Logger()
	backendOpts := backend.ApplyOptions(backend.WithLogger(
		slog.New(slogzerolog.Option{
			Level:  workflowLevel,
			Logger: &workflowLogger,
		}.NewZerologHandler()),
	))
	workflowsBackend := etcd_backend.NewBackend(workflowsStore, backendOpts)
	workflowClient := client.New(workflowsBackend)

	hostStore := host.NewStore(a.client, storeRoot)
	hostSvc := host.NewService(a.cfg, a.etcd, hostStore, orchestrator)
	if err := hostSvc.UpdateHost(ctx); err != nil {
		return fmt.Errorf("failed to record host information: %w", err)
	}
	a.hostTicker = host.NewUpdateTicker(a.logger)
	a.hostTicker.Start(ctx, hostSvc)

	dbStore := database.NewStore(a.client, storeRoot)
	dbSvc := database.NewService(orchestrator, dbStore, hostSvc)

	svc := api.NewService(a.cfg, a.logger, a.etcd, hostSvc, dbSvc, workflowClient)
	a.apiSvc.UpdateImpl(svc)

	// zerologLogger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr})

	// TODO: move to etcd backend package or logger package

	w := worker.NewWorker(a.cfg, a.logger, workflowsBackend)

	// TODO: can we combine this with the condition above?
	switch a.cfg.Orchestrator {
	case config.OrchestratorSwarm:
		fs := afero.NewOsFs()
		var loopMgr filesystem.LoopDeviceManager
		fstabMgr, err := filesystem.NewFSTabManager(filesystem.FSTabManagerOptions{
			FS:         fs,
			FileWriter: filesystem.SudoWriter,
		})
		// TODO: this is ugly, we should handle this better
		if err == nil {
			loopMgr = filesystem.NewLoopDeviceManager(filesystem.LoopDeviceManagerOptions{
				CmdRunner:    exec.RunCmd,
				FSTabManager: fstabMgr,
				FS:           fs,
			})
		} else {
			a.logger.Warn().Err(err).Msg("failed to initialize fstab manager. loop_device storage class will not be available.")
		}
		ipamSvc := ipam.NewService(a.cfg, a.logger, ipam.NewStore(a.client, storeRoot))
		if err := ipamSvc.Start(ctx); err != nil {
			return fmt.Errorf("failed to start ipam service: %w", err)
		}
		err = w.StartSwarmWorker(
			ctx,
			fs,
			loopMgr,
			ipamSvc,
			hostSvc,
			a.etcd.CertService(),
			a.docker,
			a.etcd,
			a.client,
			exec.RunCmd,
		)
		if err != nil {
			return fmt.Errorf("failed to start swarm worker: %w", err)
		}
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
	errs := []error{reason}

	if a.hostTicker != nil {
		a.logger.Info().Msg("stopping host ticker")
		a.hostTicker.Stop()
	}
	if a.api != nil {
		a.logger.Info().Msg("stopping api")
		errs = append(errs, a.api.Shutdown())
	}
	if a.client != nil {
		a.logger.Info().Msg("stopping client")
		errs = append(errs, a.client.Close())
	}
	if a.etcd != nil {
		a.logger.Info().Msg("stopping etcd")
		errs = append(errs, a.etcd.Shutdown())
	}

	return errors.Join(errs...)
}
