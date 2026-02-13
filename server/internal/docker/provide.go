package docker

import (
	"github.com/rs/zerolog"
	"github.com/samber/do"
)

func Provide(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Docker, error) {
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, err
		}
		cli, err := NewDocker(logger)
		if err != nil {
			return nil, err
		}
		return cli, nil
	})
}
