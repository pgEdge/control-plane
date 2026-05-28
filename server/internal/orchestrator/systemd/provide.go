package systemd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/logging"
	"github.com/samber/do"
)

type OSFamily string

const (
	OSFamilyUnknown OSFamily = "unknown"
	OSFamilyRedHat  OSFamily = "redhat"
	OSFamilyDebian  OSFamily = "debian"
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
		osFamily, err := getOSFamily()
		if err != nil {
			return nil, fmt.Errorf("failed to determine os family: %w", err)
		}
		switch osFamily {
		case OSFamilyRedHat:
			return &Dnf{}, nil
		case OSFamilyDebian:
			return &Apt{}, nil
		default:
			return nil, fmt.Errorf("unrecognized os family '%s'", osFamily)
		}
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

func getOSFamily() (OSFamily, error) {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return "", fmt.Errorf("failed to open /etc/os-release: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ID=") || strings.HasPrefix(line, "ID_LIKE=") {
			if strings.Contains(line, "debian") {
				return OSFamilyDebian, nil
			}
			if strings.Contains(line, "rhel") || strings.Contains(line, "fedora") {
				return OSFamilyRedHat, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to scan /etc/os-release: %w", err)
	}
	return OSFamilyUnknown, nil
}
