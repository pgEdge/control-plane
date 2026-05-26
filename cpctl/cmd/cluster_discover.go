package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/pgEdge/control-plane/client"
	"github.com/pgEdge/control-plane/common/prompt"
	"github.com/pgEdge/control-plane/cpctl/config"
)

const reachableUrlTimeout = 2 * time.Second
const discoverTimeout = 30 * time.Second

func newClusterDiscoverCommand(mgr *config.Manager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Updates the selected profile ",
		RunE: func(cmd *cobra.Command, args []string) error {
			defaultPromptAnswer, err := prompt.DefaultFromYesNoFlags(cmd.Flags())
			if err != nil {
				return err
			}
			cli, err := mgr.APIClient(args)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), discoverTimeout)
			defer cancel()

			logger := mgr.Logger()
			servers, err := discoverServers(ctx, cli, mgr, logger)
			if err != nil {
				return err
			}
			err = updateActiveProfileServers(mgr, servers, logger, defaultPromptAnswer)
			if err != nil {
				return err
			}

			return nil
		},
	}

	prompt.AddYesNoFlags(cmd)

	return cmd
}

func discoverServers(
	ctx context.Context,
	cli client.Client,
	mgr *config.Manager,
	logger zerolog.Logger,
) ([]config.Server, error) {
	resp, err := cli.ListHosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list hosts: %w", err)
	}

	httpCli, err := mgr.HTTPClient()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize http client: %w", err)
	}
	httpCli.Timeout = reachableUrlTimeout

	servers := make([]config.Server, 0, len(resp.Hosts))
	for _, host := range resp.Hosts {
		log := logger.With().Str("host_id", string(host.ID)).Logger()
		log.Debug().Msg("finding reachable URL")

		url, err := pickReachable(ctx, httpCli, host.APIClientUrls)
		if err != nil {
			log.Warn().Err(err).Msg("unable to reach host")
		} else {
			servers = append(servers, config.Server{
				ID:  string(host.ID),
				URL: url,
			})
		}
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("unable to reach any hosts by their advertised addresses")
	}

	return servers, nil
}

func updateActiveProfileServers(
	mgr *config.Manager,
	servers []config.Server,
	logger zerolog.Logger,
	defaultAnswer *bool,
) error {
	cfg := mgr.Config()
	log := logger.With().Str("profile", cfg.Profile).Logger()

	raw, err := yaml.Marshal(servers)
	if err != nil {
		return fmt.Errorf("failed to marshal servers for prompt: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Discovered servers:\n%s\n", string(raw))

	var ans bool
	if defaultAnswer != nil {
		ans = *defaultAnswer
	} else {
		ans, err = prompt.YesNo(fmt.Sprintf("Update profile '%s'?", cfg.Profile), true)
		if err != nil {
			return err
		}
	}
	if !ans {
		log.Warn().Msg("not updating profile")
		return nil
	}

	log.Info().Msg("updating profile")

	return mgr.UpdateActiveProfile(config.Profile{
		Servers: servers,
		TLS:     cfg.SelectedProfile().TLS,
	})
}

func pickReachable(ctx context.Context, cli *http.Client, urls []string) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		url string
		err error
	}

	ch := make(chan result, len(urls))

	for _, u := range urls {
		go func(u string) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if err != nil {
				ch <- result{err: err}
				return
			}
			resp, err := cli.Do(req)
			if err != nil {
				ch <- result{err: err}
				return
			}
			resp.Body.Close()
			ch <- result{url: u}
		}(u)
	}

	var errs []error
	for range urls {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case r := <-ch:
			if r.err == nil {
				return r.url, nil // cancel() fires via defer, stopping remaining goroutines
			}
			errs = append(errs, r.err)
		}
	}
	return "", errors.Join(errs...)
}
