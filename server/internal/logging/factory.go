package logging

import (
	"fmt"

	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/config"
)

type Factory struct {
	base            zerolog.Logger
	componentLevels map[string]zerolog.Level
}

func NewFactory(cfg config.Config, base zerolog.Logger) (*Factory, error) {
	componentLevels := map[string]zerolog.Level{}

	for component, l := range cfg.Logging.ComponentLevels {
		level, err := zerolog.ParseLevel(l)
		if err != nil {
			return nil, fmt.Errorf("failed to parse level for component '%s': %w", component, err)
		}
		componentLevels[component] = level
	}

	return &Factory{
		base:            base,
		componentLevels: componentLevels,
	}, nil
}

func (f *Factory) Logger(component string) zerolog.Logger {
	logger := f.base
	level, ok := f.componentLevels[component]
	if ok {
		logger = logger.Level(level)
	}

	return logger.With().Str("component", component).Logger()
}
