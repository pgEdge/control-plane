package migrate

import (
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/election"
)

const ElectionName = election.Name("migration_runner")
const LockTTL time.Duration = 30 * time.Second

// Provide registers migration dependencies with the injector.
func Provide(i *do.Injector) {
	provideStore(i)
	provideRunner(i)
}

func provideStore(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Store, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		client, err := do.Invoke[*clientv3.Client](i)
		if err != nil {
			return nil, err
		}
		return NewStore(client, cfg.EtcdKeyRoot), nil
	})
}

func provideRunner(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Runner, error) {
		store, err := do.Invoke[*Store](i)
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
		electionSvc, err := do.Invoke[*election.Service](i)
		if err != nil {
			return nil, err
		}
		migrations, err := AllMigrations()
		if err != nil {
			return nil, err
		}

		locker := electionSvc.NewCandidate(ElectionName, cfg.HostID, LockTTL)
		return NewRunner(
			cfg.HostID,
			store,
			i,
			logger,
			migrations,
			locker,
		), nil
	})
}
