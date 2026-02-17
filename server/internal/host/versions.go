package host

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

// VersionConstraint defines an optional minimum and/or maximum version bound.
// A nil Min or Max means no restriction on that end of the range.
type VersionConstraint struct {
	Min *Version `json:"min,omitempty"`
	Max *Version `json:"max,omitempty"`
}

// IsSatisfied returns true if v falls within the constraint's bounds.
func (c *VersionConstraint) IsSatisfied(v *Version) bool {
	if c.Min != nil && c.Min.Compare(v) > 0 {
		return false
	}
	if c.Max != nil && c.Max.Compare(v) < 0 {
		return false
	}
	return true
}

func (c *VersionConstraint) String() string {
	var parts []string
	if c.Min != nil {
		parts = append(parts, fmt.Sprintf(">= %s", c.Min))
	}
	if c.Max != nil {
		parts = append(parts, fmt.Sprintf("<= %s", c.Max))
	}
	if len(parts) == 0 {
		return "any"
	}
	return strings.Join(parts, " and ")
}

var _ encoding.TextMarshaler = (*Version)(nil)
var _ encoding.TextUnmarshaler = (*Version)(nil)

type Version struct {
	Components []uint64 `json:"components"`
}

func (v *Version) Major() (uint64, bool) {
	if len(v.Components) == 0 {
		return 0, false
	}
	return v.Components[0], true
}

func (v *Version) String() string {
	components := make([]string, len(v.Components))
	for i, c := range v.Components {
		components[i] = strconv.FormatUint(c, 10)
	}
	return strings.Join(components, ".")
}

func (v *Version) Clone() *Version {
	return &Version{
		Components: slices.Clone(v.Components),
	}
}

func (v *Version) MarshalText() (data []byte, err error) {
	return []byte(v.String()), nil
}

func (v *Version) UnmarshalText(data []byte) error {
	parsed, err := ParseVersion(string(data))
	if err != nil {
		return err
	}
	v.Components = parsed.Components
	return nil
}

func (v *Version) UnmarshalJSON(data []byte) error {
	// Needed temporarily for backwards compatibility. We can remove this entire
	// UnmarshalJSON function once everyone has upgraded.
	if len(data) == 0 {
		return nil
	}

	d := string(data)
	switch d[0] {
	case '{':
		var m map[string]string
		err := json.Unmarshal(data, &m)
		if err != nil {
			return err
		}
		return v.UnmarshalText([]byte(m["semver"]))
	case '"':
		var s string
		err := json.Unmarshal(data, &s)
		if err != nil {
			return err
		}
		return v.UnmarshalText([]byte(s))
	default:
		return fmt.Errorf("invalid version format: %s", data)
	}
}

func (v *Version) Compare(other *Version) int {
	return slices.Compare(v.Components, other.Components)
}

var semverRegexp = regexp.MustCompile(`^\d+(.\d+){0,2}$`)

func MustParseVersion(s string) *Version {
	v, err := ParseVersion(s)
	if err != nil {
		panic(err)
	}
	return v
}

func ParseVersion(s string) (*Version, error) {
	if !semverRegexp.MatchString(s) {
		return nil, fmt.Errorf("invalid version format: %q", s)
	}
	parts := strings.Split(s, ".")
	components := make([]uint64, len(parts))
	for i, p := range parts {
		c, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid version component %q: %w", p, err)
		}
		components[i] = c
	}
	return &Version{Components: components}, nil
}

type PgEdgeVersion struct {
	PostgresVersion *Version `json:"postgres_version"`
	SpockVersion    *Version `json:"spock_version"`
}

func (v *PgEdgeVersion) Clone() *PgEdgeVersion {
	return &PgEdgeVersion{
		PostgresVersion: v.PostgresVersion.Clone(),
		SpockVersion:    v.SpockVersion.Clone(),
	}
}

func (v *PgEdgeVersion) String() string {
	return fmt.Sprintf("%s_%s", v.PostgresVersion, v.SpockVersion)
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
	pv, err := ParseVersion(postgresVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid postgres version: %q", postgresVersion)
	}
	sv, err := ParseVersion(spockVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid spock version: %q", spockVersion)
	}

	return &PgEdgeVersion{
		PostgresVersion: pv,
		SpockVersion:    sv,
	}, nil
}

func GreatestCommonDefaultVersion(hosts ...*Host) (*PgEdgeVersion, error) {
	// We can't do set operations on *PgEdgeVersion, and we can't do semver
	// comparisons on strings. So, we'll use strings for set operations, then
	// translate them back to *PgEdgeVersions to do the version comparisons.
	stringToVersion := map[string]*PgEdgeVersion{}
	defaultVersions := ds.NewSet[string]()
	var commonVersions ds.Set[string]
	for _, h := range hosts {
		defaultVersions.Add(h.DefaultPgEdgeVersion.String())
		supported := ds.NewSet[string]()
		for _, v := range h.SupportedPgEdgeVersions {
			vs := v.String()
			supported.Add(vs)
			stringToVersion[vs] = v
		}
		if commonVersions == nil {
			commonVersions = supported
		} else {
			commonVersions = commonVersions.Intersection(supported)
		}
	}

	commonDefaults := defaultVersions.Intersection(commonVersions)
	if len(commonDefaults) == 0 {
		return nil, errors.New("no common default versions found between the given hosts")
	}

	versions := make([]*PgEdgeVersion, 0, len(commonDefaults))
	for vs := range commonDefaults {
		v, ok := stringToVersion[vs]
		if !ok {
			return nil, fmt.Errorf("invalid state - missing version: %q", vs)
		}
		versions = append(versions, v)
	}
	slices.SortFunc(versions, func(a, b *PgEdgeVersion) int {
		// Sort in reverse order
		return -a.Compare(b)
	})
	return versions[0], nil
}
