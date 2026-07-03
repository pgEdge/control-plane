package swarm

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
)

type Images struct {
	PgEdgeImage string
	// Stability is "stable" or "dev". Empty string is treated as "stable".
	Stability string
}

type Versions struct {
	cfg               config.Config
	supportedVersions []*ds.PgEdgeVersion
	defaultVersion    *ds.PgEdgeVersion
	images            map[string]map[string]*Images
}


func (v Versions) Supported() []*ds.PgEdgeVersion {
	return v.supportedVersions
}

func (v Versions) Default() *ds.PgEdgeVersion {
	return v.defaultVersion
}

func (v *Versions) addImage(version *ds.PgEdgeVersion, images *Images) {
	pgv := version.PostgresVersion.String()
	sv := version.SpockVersion.String()

	if _, ok := v.images[pgv]; !ok {
		v.images[pgv] = make(map[string]*Images)
	}

	v.images[pgv][sv] = images
	v.supportedVersions = append(v.supportedVersions, version)
}

func (v Versions) GetImages(version *ds.PgEdgeVersion) (*Images, error) {
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

// FindByImage returns the PgEdgeVersion and Images for the manifest entry
// whose PgEdgeImage matches image exactly. Returns (nil, nil, false) when no
// entry matches.
func (v *Versions) FindByImage(image string) (*ds.PgEdgeVersion, *Images, bool) {
	for _, ver := range v.supportedVersions {
		img, err := v.GetImages(ver)
		if err != nil {
			continue
		}
		if img.PgEdgeImage == image {
			return ver, img, true
		}
	}
	return nil, nil, false
}

// AvailableUpgrades returns all newer stable manifest entries in the same
// (postgres_major, spock_major) bucket as current. Returns nil when current is
// nil or no newer entries exist.
func (v *Versions) AvailableUpgrades(current *ds.PgEdgeVersion) []*database.AvailableUpgrade {
	if current == nil {
		return nil
	}
	currentPGMajor, ok := current.PostgresVersion.Major()
	if !ok {
		return nil
	}
	currentSpockMajor, ok := current.SpockVersion.Major()
	if !ok {
		return nil
	}

	var upgrades []*database.AvailableUpgrade
	for _, ver := range v.supportedVersions {
		pgMajor, ok := ver.PostgresVersion.Major()
		if !ok || pgMajor != currentPGMajor {
			continue
		}
		spockMajor, ok := ver.SpockVersion.Major()
		if !ok || spockMajor != currentSpockMajor {
			continue
		}
		if ver.PostgresVersion.Compare(current.PostgresVersion) <= 0 {
			continue
		}
		img, err := v.GetImages(ver)
		if err != nil {
			continue
		}
		if img.Stability != "" && img.Stability != "stable" {
			continue
		}
		upgrades = append(upgrades, &database.AvailableUpgrade{
			PostgresVersion: ver.PostgresVersion.String(),
			SpockVersion:    ver.SpockVersion.String(),
			Image:           img.PgEdgeImage,
		})
	}
	return upgrades
}
