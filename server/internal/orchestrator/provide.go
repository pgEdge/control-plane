package orchestrator

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/swarm"
	"github.com/pgEdge/control-plane/server/internal/workflows"
	"github.com/samber/do"
)

type Orchestrator interface {
	host.Orchestrator
	database.Orchestrator
	workflows.Orchestrator
}

func Provide(i *do.Injector) error {
	cfg, err := do.Invoke[config.Config](i)
	if err != nil {
		return err
	}
	switch cfg.Orchestrator {
	case config.OrchestratorSwarm:
		swarm.Provide(i)
		provideOrchestrator[*swarm.Orchestrator](i)
	default:
		return fmt.Errorf("unsupported orchestrator: %q", cfg.Orchestrator)
	}

	return nil
}

// A downside of this injector library is that it uses type names (as opposed to
// interface checks) to pick the right implementation. It keeps the injector
// simple, but it means that we need to register the same implementation under
// each interface it implements. These functions use some type trickery to make
// this less verbose once we have multiple orchestrator implementations.
func provideOrchestrator[T Orchestrator](i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (Orchestrator, error) {
		o, err := do.Invoke[T](i)
		if err != nil {
			return nil, err
		}
		return o, nil
	})
	provideHostOrchestrator[T](i)
	provideDatabaseOrchestrator[T](i)
	provideWorkflowsOrchestrator[T](i)
}

func provideHostOrchestrator[T host.Orchestrator](i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (host.Orchestrator, error) {
		o, err := do.Invoke[T](i)
		if err != nil {
			return nil, err
		}
		return o, nil
	})
}

func provideDatabaseOrchestrator[T database.Orchestrator](i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (database.Orchestrator, error) {
		o, err := do.Invoke[T](i)
		if err != nil {
			return nil, err
		}
		return o, nil
	})
}

func provideWorkflowsOrchestrator[T workflows.Orchestrator](i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (workflows.Orchestrator, error) {
		o, err := do.Invoke[T](i)
		if err != nil {
			return nil, err
		}
		return o, nil
	})
}
