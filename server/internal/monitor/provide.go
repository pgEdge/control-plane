package monitor

import (
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/election"
	"github.com/pgEdge/control-plane/server/internal/host"
)

const electionName election.Name = "databases-monitor"
const electionTTL time.Duration = 30 * time.Second

func Provide(i *do.Injector) {
	provideStore(i)
	provideService(i)
}

func provideService(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Service, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, err
		}
		dbSvc, err := do.Invoke[*database.Service](i)
		if err != nil {
			return nil, err
		}
		certSvc, err := do.Invoke[*certificates.Service](i)
		if err != nil {
			return nil, err
		}
		dbOrch, err := do.Invoke[database.Orchestrator](i)
		if err != nil {
			return nil, err
		}
		store, err := do.Invoke[*Store](i)
		if err != nil {
			return nil, err
		}
		hostSvc, err := do.Invoke[*host.Service](i)
		if err != nil {
			return nil, err
		}
		electionSvc, err := do.Invoke[*election.Service](i)
		if err != nil {
			return nil, err
		}

		candidate := electionSvc.NewCandidate(electionName, cfg.HostID, electionTTL)
		return NewService(cfg, logger, dbSvc, certSvc, dbOrch, store, hostSvc, candidate), nil
	})
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
