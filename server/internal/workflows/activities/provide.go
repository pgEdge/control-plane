package activities

import (
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
)

func Provide(i *do.Injector) {
	provideActivities(i)
}

func provideActivities(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Activities, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		orch, err := do.Invoke[database.Orchestrator](i)
		if err != nil {
			return nil, err
		}

		return &Activities{
			Config:       cfg,
			Injector:     i,
			Orchestrator: orch,
		}, nil
	})
}
