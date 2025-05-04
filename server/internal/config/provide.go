package config

import "github.com/samber/do"

func Provide(i *do.Injector, config Config) {
	do.Provide(i, func(_ *do.Injector) (Config, error) {
		return config, nil
	})
}
