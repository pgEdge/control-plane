package host

import (
	"fmt"
	"slices"

	"github.com/Masterminds/semver/v3"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

type PgEdgeVersion struct {
	PostgresVersion *semver.Version `json:"postgres_version"`
	SpockVersion    *semver.Version `json:"spock_version"`
}

func (v *PgEdgeVersion) String() string {
	return fmt.Sprintf("%s-%s", v.PostgresVersion.String(), v.SpockVersion.String())
}

func (v *PgEdgeVersion) Compare(other *PgEdgeVersion) int {
	if c := v.PostgresVersion.Compare(other.PostgresVersion); c != 0 {
		return c
	}
	return v.SpockVersion.Compare(other.SpockVersion)
}

func (v *PgEdgeVersion) Equals(other *PgEdgeVersion) bool {
	return v.Compare(other) == 0
}

func (v *PgEdgeVersion) LessThan(other *PgEdgeVersion) bool {
	return v.Compare(other) < 0
}

func (v *PgEdgeVersion) GreaterThan(other *PgEdgeVersion) bool {
	return v.Compare(other) > 0
}

func MustPgEdgeVersion(postgresVersion, spockVersion string) *PgEdgeVersion {
	v, err := NewPgEdgeVersion(postgresVersion, spockVersion)
	if err != nil {
		panic(err)
	}
	return v
}

func NewPgEdgeVersion(postgresVersion, spockVersion string) (*PgEdgeVersion, error) {
	pv, err := semver.NewVersion(postgresVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid postgres version: %w", err)
	}
	sv, err := semver.NewVersion(spockVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid spock version: %w", err)
	}
	return &PgEdgeVersion{
		PostgresVersion: pv,
		SpockVersion:    sv,
	}, nil
}

// func pgEdgeVersionFromStrings(postgresVersion, spockVersion string) (PgEdgeVersion, error) {
// 	pv, err := semver.NewVersion(postgresVersion)
// 	if err != nil {
// 		return PgEdgeVersion{}, fmt.Errorf("invalid postgres version: %w", err)
// 	}
// 	sv, err := semver.NewVersion(spockVersion)
// 	if err != nil {
// 		return PgEdgeVersion{}, fmt.Errorf("invalid spock version: %w", err)
// 	}
// 	return PgEdgeVersion{
// 		PostgresVersion: pv,
// 		SpockVersion:    sv,
// 	}, nil
// }

func GreatestCommonDefaultVersion(hosts ...*Host) (*PgEdgeVersion, error) {
	allVersions := map[string]*PgEdgeVersion{}
	commonSet := ds.NewSet[string]()
	supported := make([][]*PgEdgeVersion, 0, len(hosts))
	for _, h := range hosts {
		s := h.DefaultPgEdgeVersion.String()
		allVersions[s] = h.DefaultPgEdgeVersion
		commonSet.Add(s)
	}
	for _, sv := range supported {
		svSet := ds.NewSet[string]()
		for _, v := range sv {
			allVersions[v.String()] = v
			svSet.Add(v.String())
		}
		commonSet = commonSet.Intersection(svSet)
		if len(commonSet) == 0 {
			// short-circuit as soon as we know there are no common versions
			break
		}
	}
	if len(commonSet) == 0 {
		return nil, fmt.Errorf("no common versions found")
	}
	common := make([]*PgEdgeVersion, 0, len(commonSet))
	for versionString := range commonSet {
		version, ok := allVersions[versionString]
		if !ok {
			return nil, fmt.Errorf("invalid state - missing version: %q", versionString)
		}
		common = append(common, version)
	}
	slices.SortFunc(common, func(a, b *PgEdgeVersion) int {
		// Sort in reverse order
		return -a.Compare(b)
	})
	return common[0], nil
}

// func (sv SupportedVersions) Supports(postgresVersion PostgresVersion, spockVersion SpockVersion) bool {
// 	if _, ok := sv[postgresVersion]; !ok {
// 		return false
// 	}
// 	return sv[postgresVersion][spockVersion]
// }

// func (sv SupportedVersions) All() ds.Set[PgEdgeVersion] {
// 	versions := ds.NewSet[PgEdgeVersion]()
// 	for postgresVersion, spockVersions := range sv {
// 		for spockVersion := range spockVersions {
// 			versions.Add(PgEdgeVersion{
// 				PostgresVersion: postgresVersion,
// 				SpockVersion:    spockVersion,
// 			})
// 		}
// 	}
// 	return versions
// }

// func (sv SupportedVersions) CommonVersions(others ...SupportedVersions) ds.Set[PgEdgeVersion] {
// 	commonVersions := sv.All()
// 	for _, other := range others {
// 		commonVersions = commonVersions.Intersection(other.All())
// 	}
// 	return commonVersions
// }
