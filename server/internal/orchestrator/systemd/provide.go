package systemd

import (
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/samber/do"
)

func Provide(i *do.Injector) {
	provideClient(i)
	providePackageManager(i)
	provideOrchestrator(i)
}

func provideClient(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Client, error) {
		loggerFactory, err := do.Invoke[*logging.Factory](i)
		if err != nil {
			return nil, err
		}

		return NewClient(loggerFactory), nil
	})
}

func providePackageManager(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (PackageManager, error) {
		// TODO: add a function to check whether OS is RHEL-like or debian-like
		// and return the appropriate package manager implementation.
		return &Dnf{}, nil
	})
}

func provideOrchestrator(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Orchestrator, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		loggerFactory, err := do.Invoke[*logging.Factory](i)
		if err != nil {
			return nil, err
		}
		client, err := do.Invoke[*Client](i)
		if err != nil {
			return nil, err
		}
		packageManager, err := do.Invoke[PackageManager](i)
		if err != nil {
			return nil, err
		}

		return NewOrchestrator(cfg, loggerFactory, client, packageManager)
	})
}
