package logging

import (
	"fmt"

	"github.com/rs/zerolog"
)

type Component string

func (c Component) String() string {
	return string(c)
}

type Factory struct {
	base            zerolog.Logger
	componentLevels map[Component]zerolog.Level
}

func NewFactory(levels map[Component]string, base zerolog.Logger) (*Factory, error) {
	componentLevels := map[Component]zerolog.Level{}

	for component, l := range levels {
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
