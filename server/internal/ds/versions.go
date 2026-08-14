package ds

import (
	"encoding"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
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
	// PreRelease is the optional suffix after a "-" (e.g. "beta.1" in
	// "6.0.0-beta.1"). It is an opaque string, not decomposed into SemVer
	// precedence rules, and is intentionally dropped by MajorVersion and
	// MajorMinorVersion since a major/major-minor bucket never carries one.
	PreRelease string `json:"pre_release,omitempty"`
}

func (v *Version) Major() (uint64, bool) {
	if len(v.Components) == 0 {
		return 0, false
	}
	return v.Components[0], true
}

func (v *Version) MajorString() (string, bool) {
	major, ok := v.Major()
	if !ok {
		return "", false
	}
	return strconv.FormatUint(major, 10), true
}

// MajorVersion returns just the major component. PreRelease is intentionally
// dropped: a major-version bucket never carries a pre-release suffix.
func (v *Version) MajorVersion() *Version {
	if len(v.Components) == 0 {
		return &Version{}
	}
	return &Version{
		Components: slices.Clone(v.Components[:1]),
	}
}

// MajorMinorVersion returns the major.minor components. PreRelease is
// intentionally dropped: a major.minor bucket never carries a pre-release
// suffix.
func (v *Version) MajorMinorVersion() *Version {
	components := slices.Clone(v.Components)
	if len(components) > 2 {
		components = components[:2]
	}
	return &Version{
		Components: components,
	}
}

func (v *Version) String() string {
	components := make([]string, len(v.Components))
	for i, c := range v.Components {
		components[i] = strconv.FormatUint(c, 10)
	}
	s := strings.Join(components, ".")
	if v.PreRelease != "" {
		s += "-" + v.PreRelease
	}
	return s
}

func (v *Version) Clone() *Version {
	return &Version{
		Components: slices.Clone(v.Components),
		PreRelease: v.PreRelease,
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
	v.PreRelease = parsed.PreRelease
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

// Compare orders by numeric components first. If those are equal, a
// pre-release never compares equal to its release counterpart (a release
// sorts after any pre-release of the same numeric version), and two
// different pre-releases of the same numeric version fall back to a plain
// string comparison. This is NOT full SemVer pre-release precedence — it's
// just enough to avoid falsely claiming two different versions are equal.
func (v *Version) Compare(other *Version) int {
	if c := slices.Compare(v.Components, other.Components); c != 0 {
		return c
	}
	switch {
	case v.PreRelease == other.PreRelease:
		return 0
	case v.PreRelease == "":
		return 1
	case other.PreRelease == "":
		return -1
	default:
		return strings.Compare(v.PreRelease, other.PreRelease)
	}
}

// semverRegexp matches "<major>[.<minor>[.<patch>]][-<prerelease>]". The
// pre-release group accepts dot/hyphen-separated alphanumeric identifiers
// (e.g. "beta", "beta.1", "rc.2") but not SemVer build metadata ("+...").
var semverRegexp = regexp.MustCompile(`^(\d+(?:\.\d+){0,2})(?:-([0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*))?$`)

func MustParseVersion(s string) *Version {
	v, err := ParseVersion(s)
	if err != nil {
		panic(err)
	}
	return v
}

func ParseVersion(s string) (*Version, error) {
	m := semverRegexp.FindStringSubmatch(s)
	if m == nil {
		return nil, fmt.Errorf("invalid version format: %q", s)
	}
	parts := strings.Split(m[1], ".")
	components := make([]uint64, len(parts))
	for i, p := range parts {
		c, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid version component %q: %w", p, err)
		}
		components[i] = c
	}
	return &Version{Components: components, PreRelease: m[2]}, nil
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

// Normalize returns the Postgres version in major.minor format and the Spock
// version in major format. This matches the way that versions are currently
// provided in our API.
func (v *PgEdgeVersion) Normalize() (*PgEdgeVersion, error) {
	pv := v.PostgresVersion.MajorMinorVersion()
	if len(pv.Components) != 2 {
		return nil, fmt.Errorf("expected at least a major and minor version for postgres, got '%s'", pv)
	}
	sv := v.SpockVersion.MajorVersion()
	if len(sv.Components) != 1 {
		return nil, fmt.Errorf("expected at least a major version for spock, got '%s'", sv)
	}

	return &PgEdgeVersion{
		PostgresVersion: pv,
		SpockVersion:    sv,
	}, nil
}

func MustParsePgEdgeVersion(postgresVersion, spockVersion string) *PgEdgeVersion {
	v, err := ParsePgEdgeVersion(postgresVersion, spockVersion)
	if err != nil {
		panic(err)
	}
	return v
}

func ParsePgEdgeVersion(postgresVersion, spockVersion string) (*PgEdgeVersion, error) {
	pv, err := ParseVersion(postgresVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid postgres version: '%s'", postgresVersion)
	}
	sv, err := ParseVersion(spockVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid spock version: '%s'", spockVersion)
	}
	return &PgEdgeVersion{
		PostgresVersion: pv,
		SpockVersion:    sv,
	}, nil
}
