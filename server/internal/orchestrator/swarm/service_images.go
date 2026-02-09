package swarm

import (
	"fmt"
	"strings"

	"github.com/pgEdge/control-plane/server/internal/config"
)

type ServiceImages struct {
	Image string
}

type ServiceVersions struct {
	cfg    config.Config
	images map[string]map[string]*ServiceImages
}

func NewServiceVersions(cfg config.Config) *ServiceVersions {
	versions := &ServiceVersions{
		cfg:    cfg,
		images: make(map[string]map[string]*ServiceImages),
	}

	// MCP service versions
	// TODO: there is no "1.0.0" image yet - the latest is something like "1.0.0-beta3"
	versions.addServiceImage("mcp", "1.0.0", &ServiceImages{
		Image: serviceImageTag(cfg, "postgres-mcp:1.0.0"),
	})
	versions.addServiceImage("mcp", "latest", &ServiceImages{
		Image: serviceImageTag(cfg, "postgres-mcp:latest"),
	})

	return versions
}

func (sv *ServiceVersions) addServiceImage(serviceType string, version string, images *ServiceImages) {
	if _, ok := sv.images[serviceType]; !ok {
		sv.images[serviceType] = make(map[string]*ServiceImages)
	}

	sv.images[serviceType][version] = images
}

func (sv *ServiceVersions) GetServiceImage(serviceType string, version string) (string, error) {
	versionMap, ok := sv.images[serviceType]
	if !ok {
		return "", fmt.Errorf("unsupported service type %q", serviceType)
	}

	images, ok := versionMap[version]
	if !ok {
		return "", fmt.Errorf("unsupported version %q for service type %q", version, serviceType)
	}

	return images.Image, nil
}

func (sv *ServiceVersions) SupportedServiceVersions(serviceType string) ([]string, error) {
	versionMap, ok := sv.images[serviceType]
	if !ok {
		return nil, fmt.Errorf("unsupported service type %q", serviceType)
	}

	versions := make([]string, 0, len(versionMap))
	for version := range versionMap {
		versions = append(versions, version)
	}

	return versions, nil
}

func serviceImageTag(cfg config.Config, imageRef string) string {
	// If the image reference already contains a registry (has a slash before the first colon),
	// use it as-is. Otherwise, prepend the configured repository host.
	if strings.Contains(imageRef, "/") {
		// Image already has a registry or organization prefix
		parts := strings.Split(imageRef, "/")
		firstPart := parts[0]
		// Check if first part looks like a registry (has a dot, colon, or is localhost)
		if strings.Contains(firstPart, ".") || strings.Contains(firstPart, ":") || firstPart == "localhost" {
			// First part looks like a registry
			return imageRef
		}
	}

	// Prepend repository host
	return fmt.Sprintf("%s/%s", cfg.DockerSwarm.ImageRepositoryHost, imageRef)
}
