package swarm

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/host"
)

type Images struct {
	PgEdgeImage string
}

type Versions struct {
	cfg               config.Config
	supportedVersions []*host.PgEdgeVersion
	defaultVersion    *host.PgEdgeVersion
	images            map[string]map[string]*Images
}

func NewVersions(cfg config.Config) *Versions {
	versions := &Versions{
		cfg:    cfg,
		images: make(map[string]map[string]*Images),
	}

	// pg16
	versions.addImage(host.MustPgEdgeVersion("16.10", "5"), &Images{
		PgEdgeImage: imageTag(cfg, "16.10-spock5.0.4-standard-2"),
	})
	versions.addImage(host.MustPgEdgeVersion("16.11", "5"), &Images{
		PgEdgeImage: imageTag(cfg, "16.11-spock5.0.4-standard-3"),
	})

	// pg17
	versions.addImage(host.MustPgEdgeVersion("17.6", "5"), &Images{
		PgEdgeImage: imageTag(cfg, "17.6-spock5.0.4-standard-2"),
	})
	versions.addImage(host.MustPgEdgeVersion("17.7", "5"), &Images{
		PgEdgeImage: imageTag(cfg, "17.7-spock5.0.4-standard-3"),
	})

	// pg18
	versions.addImage(host.MustPgEdgeVersion("18.0", "5"), &Images{
		PgEdgeImage: imageTag(cfg, "18.0-spock5.0.4-standard-2"),
	})
	versions.addImage(host.MustPgEdgeVersion("18.1", "5"), &Images{
		PgEdgeImage: imageTag(cfg, "18.1-spock5.0.4-standard-3"),
	})

	versions.defaultVersion = host.MustPgEdgeVersion("18.1", "5")

	return versions
}

func (v *Versions) Supported() []*host.PgEdgeVersion {
	return v.supportedVersions
}

func (v *Versions) Default() *host.PgEdgeVersion {
	return v.defaultVersion
}

func (v *Versions) addImage(version *host.PgEdgeVersion, images *Images) {
	pgv := version.PostgresVersion.String()
	sv := version.SpockVersion.String()

	if _, ok := v.images[pgv]; !ok {
		v.images[pgv] = make(map[string]*Images)
	}

	v.images[pgv][sv] = images
	v.supportedVersions = append(v.supportedVersions, version)
}

func (v *Versions) GetImages(version *host.PgEdgeVersion) (*Images, error) {
	pgv := version.PostgresVersion.String()
	sv := version.SpockVersion.String()

	m, ok := v.images[pgv]
	if !ok {
		return nil, fmt.Errorf("unsupported postgres version %s", pgv)
	}

	images, ok := m[sv]
	if !ok {
		return nil, fmt.Errorf("unsupported spock version %s", sv)
	}

	return images, nil
}

func imageTag(cfg config.Config, tag string) string {
	return fmt.Sprintf("%s/pgedge-postgres:%s", cfg.DockerSwarm.ImageRepositoryHost, tag)
}
