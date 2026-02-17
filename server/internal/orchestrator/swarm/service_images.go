package swarm

import (
	"fmt"
	"strings"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/host"
)

// ServiceImage describes a container image for a service type+version, along
// with optional version constraints that restrict which Postgres and Spock
// versions the service is compatible with.
type ServiceImage struct {
	Tag                string                  `json:"tag"`
	PostgresConstraint *host.VersionConstraint `json:"postgres_constraint,omitempty"`
	SpockConstraint    *host.VersionConstraint `json:"spock_constraint,omitempty"`
}

// ValidateCompatibility checks that the given Postgres and Spock versions
// satisfy this image's version constraints. Returns nil if compatible.
func (s *ServiceImage) ValidateCompatibility(postgres, spock *host.Version) error {
	if s.PostgresConstraint != nil && !s.PostgresConstraint.IsSatisfied(postgres) {
		return fmt.Errorf("postgres version %s does not satisfy constraint %s",
			postgres, s.PostgresConstraint)
	}
	if s.SpockConstraint != nil && !s.SpockConstraint.IsSatisfied(spock) {
		return fmt.Errorf("spock version %s does not satisfy constraint %s",
			spock, s.SpockConstraint)
	}
	return nil
}

type ServiceVersions struct {
	cfg    config.Config
	images map[string]map[string]*ServiceImage
}

func NewServiceVersions(cfg config.Config) *ServiceVersions {
	versions := &ServiceVersions{
		cfg:    cfg,
		images: make(map[string]map[string]*ServiceImage),
	}

	// MCP service versions
	// TODO: Register semver versions when official releases are published.
	versions.addServiceImage("mcp", "latest", &ServiceImage{
		Tag: serviceImageTag(cfg, "postgres-mcp:latest"),
		// No constraints — MCP works with all PG/Spock versions.
	})

	// Example of a service image with version constraints (nil = no restriction):
	//
	//   acme-service:1.0.0 requires PG 14-17 and Spock >= 4.0.0
	//
	//   versions.addServiceImage("acme", "1.0.0", &ServiceImage{
	//       Tag: serviceImageTag(cfg, "acme-service:1.0.0"),
	//       PostgresConstraint: &host.VersionConstraint{
	//           Min: host.MustParseVersion("14"),
	//           Max: host.MustParseVersion("17"),
	//       },
	//       SpockConstraint: &host.VersionConstraint{
	//           Min: host.MustParseVersion("4.0.0"),
	//       },
	//   })

	return versions
}

func (sv *ServiceVersions) addServiceImage(serviceType string, version string, image *ServiceImage) {
	if _, ok := sv.images[serviceType]; !ok {
		sv.images[serviceType] = make(map[string]*ServiceImage)
	}

	sv.images[serviceType][version] = image
}

// GetServiceImage returns the full ServiceImage for the given service type and version.
func (sv *ServiceVersions) GetServiceImage(serviceType string, version string) (*ServiceImage, error) {
	versionMap, ok := sv.images[serviceType]
	if !ok {
		return nil, fmt.Errorf("unsupported service type %q", serviceType)
	}

	image, ok := versionMap[version]
	if !ok {
		return nil, fmt.Errorf("unsupported version %q for service type %q", version, serviceType)
	}

	return image, nil
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

	// Prepend repository host if configured
	if cfg.DockerSwarm.ImageRepositoryHost == "" {
		return imageRef
	}
	return fmt.Sprintf("%s/%s", cfg.DockerSwarm.ImageRepositoryHost, imageRef)
}
