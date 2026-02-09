package docker

import (
	"github.com/rs/zerolog"
	"github.com/samber/do"
)

func Provide(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Docker, error) {
		logger := do.MustInvoke[zerolog.Logger](i)
		cli, err := NewDocker(logger)
		if err != nil {
			return nil, err
		}
		return cli, nil
	})
}
