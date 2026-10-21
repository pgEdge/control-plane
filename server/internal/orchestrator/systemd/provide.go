package systemd

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/samber/do"
	"github.com/spf13/afero"
)

func Provide(i *do.Injector) {
	provideClient(i)
	providePackageManager(i)
	provideVersionsProbe(i)
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
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		switch cfg.SystemD.OSFamily {
		case config.OSFamilyRedHat:
			return &Dnf{}, nil
		case config.OSFamilyDebian:
			return &Apt{}, nil
		default:
			return nil, fmt.Errorf("unrecognized os family '%s'", cfg.SystemD.OSFamily)
		}
	})
}

func provideVersionsProbe(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*InstalledVersionsProbe, error) {
		fsys, err := do.Invoke[afero.Fs](i)
		if err != nil {
			return nil, err
		}
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		packageManager, err := do.Invoke[PackageManager](i)
		if err != nil {
			return nil, err
		}
		return NewInstalledVersionsProbe(DefaultExecCommand, fsys, cfg.SystemD.PgBinDirs, packageManager), nil
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
		versionsProbe, err := do.Invoke[*InstalledVersionsProbe](i)
		if err != nil {
			return nil, err
		}

		return NewOrchestrator(cfg, loggerFactory, client, packageManager, versionsProbe)
	})
}
