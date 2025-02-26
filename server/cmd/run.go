package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/pgEdge/control-plane/server/internal/app"
)

func newRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return app.NewApp(configMgr.Config(), logger).Run(ctx)

			// etcd.RegisterEtcd(i)
			// api.RegisterServer(i)

			// logger, err := do.Invoke[zerolog.Logger](i)
			// if err != nil {
			// 	return err
			// }
			// e, err := do.Invoke[*etcd.EmbeddedEtcd](i)
			// if err != nil {
			// 	return err
			// }
			// server, err := do.Invoke[*api.Server](i)
			// if err != nil {
			// 	return err
			// }

			// initialized, err := e.IsInitialized()
			// if err != nil {
			// 	return err
			// }
			// if initialized {
			// 	e.Start(ctx)
			// }

			// server.Start(ctx)

			// mgr, err := do.Invoke[*config.Manager](i)
			// if err != nil {
			// 	return err
			// }
			// logger, err := do.Invoke[zerolog.Logger](i)
			// if err != nil {
			// 	return err
			// }

			// cfg := mgr.Config()

			// etcd, err := do.Invoke[etcd.Etcd](i)
			// if err != nil {
			// 	return err
			// }

			// initialized, err := etcd.IsInitialized()
			// if err != nil {
			// 	return err
			// }

			// switch cfg.StorageType {
			// case config.StorageTypeEmbeddedEtcd:
			// 	etcd.RegisterEmbeddedEtcd(i)
			// case config.StorageTypeRemoteEtcd:
			// 	etcd.RegisterRemoteEtcd(i)
			// }

			// cfg := configMgr.Config()
			// server := api.NewServer(configMgr.Config(), logger)
			// server.Start(ctx)

			// embedded := etcd.NewEmbeddedEtcd(cfg, logger)
			// embedded.Start(ctx)

			// client, err := embedded.GetClient()
			// if err != nil {

			// }

			// shutdown := func() {
			// 	logger.Info().Msg("attempting to shut gracefully. press ctrl+c again to force shutdown.")

			// 	gracePeriod := time.Second * time.Duration(cfg.StopGracePeriodSeconds)
			// 	shutdownCtx, cancel := context.WithTimeout(context.Background(), gracePeriod)
			// 	defer cancel()

			// 	if err := server.Stop(shutdownCtx); err != nil {
			// 		logger.Err(err).Msg("error while stopping the server")
			// 	}

			// 	embedded.Stop()
			// }

			// select {
			// case <-ctx.Done():
			// 	logger.Info().Msg("got shutdown signal")
			// 	return nil

			// // 	shutdown()
			// case err := <-server.Error():
			// 	// shutdown()

			// 	return err
			// 	// TODO: forward this channel like we do in the server
			// 	// case err := <-e.Error():
			// 	// 	// shutdown()

			// 	// 	return err
			// }

			// return nil
		},
	}
}
