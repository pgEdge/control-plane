package logging

import (
	"fmt"

	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/config"
)

type Component string

func (c Component) String() string {
	return string(c)
}

const (
	ComponentAPIServer         Component = "api_server"
	ComponentElectionCandidate Component = "election_candidate"
	ComponentEmbeddedEtcd      Component = "embedded_etcd"
	ComponentMigration         Component = "migration"
	ComponentMigrationRunner   Component = "migration_runner"
	ComponentPortsService      Component = "ports_service"
	ComponentRemoteEtcd        Component = "remote_etcd"
	ComponentSchedulerService  Component = "scheduler_service"
	ComponentWorkflowsBackend  Component = "workflows_backend"
	ComponentWorkflowsWorker   Component = "workflows_worker"
)

type Factory struct {
	base            zerolog.Logger
	componentLevels map[Component]zerolog.Level
}

func NewFactory(cfg config.Config, base zerolog.Logger) (*Factory, error) {
	componentLevels := map[Component]zerolog.Level{}

	for component, l := range cfg.Logging.ComponentLevels {
		level, err := zerolog.ParseLevel(l)
		if err != nil {
			return nil, fmt.Errorf("failed to parse level for component '%s': %w", component, err)
		}
		componentLevels[Component(component)] = level
	}

	return &Factory{
		base:            base,
		componentLevels: componentLevels,
	}, nil
}

func (f *Factory) Logger(component Component) zerolog.Logger {
	logger := f.base
	level, ok := f.componentLevels[component]
	if ok {
		logger = logger.Level(level)
	}

	return logger.With().Stringer("component", component).Logger()
}
