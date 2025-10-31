package config

import "github.com/samber/do"

func UpdateInjectedConfig(i *do.Injector) {
	do.Override(i, configProvider)
}

func Provide(i *do.Injector, sources ...*Source) {
	provideManager(i, sources...)
	provideConfig(i)
}

func provideManager(i *do.Injector, sources ...*Source) {
	do.Provide(i, func(_ *do.Injector) (*Manager, error) {
		return NewManager(sources...), nil
	})
}

func provideConfig(i *do.Injector) {
	do.Provide(i, configProvider)
}

func configProvider(i *do.Injector) (Config, error) {
	manager, err := do.Invoke[*Manager](i)
	if err != nil {
		return Config{}, err
	}
	if err := manager.Load(); err != nil {
		return Config{}, err
	}

	return manager.Config(), nil
}
