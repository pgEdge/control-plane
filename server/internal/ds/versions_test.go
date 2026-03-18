package ds_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

func TestMustParseVersion(t *testing.T) {
	t.Run("valid version", func(t *testing.T) {
		v := ds.MustParseVersion("17.6")
		assert.Equal(t, &ds.Version{Components: []uint64{17, 6}}, v)
	})

	t.Run("valid semver", func(t *testing.T) {
		v := ds.MustParseVersion("4.0.0")
		assert.Equal(t, &ds.Version{Components: []uint64{4, 0, 0}}, v)
	})

	t.Run("single component", func(t *testing.T) {
		v := ds.MustParseVersion("14")
		assert.Equal(t, &ds.Version{Components: []uint64{14}}, v)
	})

	t.Run("invalid version panics", func(t *testing.T) {
		assert.Panics(t, func() {
			ds.MustParseVersion("invalid")
		})
	})
}

func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		input       string
		expected    *ds.Version
		expectedErr string
	}{
		{
			input:    "17",
			expected: &ds.Version{Components: []uint64{17}},
		},
		{
			input:    "17.6",
			expected: &ds.Version{Components: []uint64{17, 6}},
		},
		{
			input:    "5.0.0",
			expected: &ds.Version{Components: []uint64{5, 0, 0}},
		},
		{
			input:       "5.",
			expectedErr: "invalid version format",
		},
		{
			input:       "invalid",
			expectedErr: "invalid version format",
		},
		{
			// Intentionally not supporting pre-release identifiers because they
			// are not comparable.
			input:       "5.0.0-beta",
			expectedErr: "invalid version format",
		},
	} {
		t.Run(tc.input, func(t *testing.T) {
			result, err := ds.ParseVersion(tc.input)
			if tc.expectedErr != "" {
				assert.Nil(t, result)
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		for _, tc := range []string{
			"17",
			"17.6",
			"5.0.0",
		} {
			t.Run(tc, func(t *testing.T) {
				out, err := ds.ParseVersion(tc)
				require.NoError(t, err)

				assert.Equal(t, tc, out.String())
			})
		}
	})

	t.Run("Compare", func(t *testing.T) {
		for _, tc := range []struct {
			a        *ds.Version
			b        *ds.Version
			expected int
		}{
			{
				a:        &ds.Version{Components: []uint64{17}},
				b:        &ds.Version{Components: []uint64{17}},
				expected: 0,
			},
			{
				a:        &ds.Version{Components: []uint64{17}},
				b:        &ds.Version{Components: []uint64{18}},
				expected: -1,
			},
			{
				a:        &ds.Version{Components: []uint64{18}},
				b:        &ds.Version{Components: []uint64{17}},
				expected: 1,
			},
			{
				a:        &ds.Version{Components: []uint64{17, 6}},
				b:        &ds.Version{Components: []uint64{17, 6}},
				expected: 0,
			},
			{
				a:        &ds.Version{Components: []uint64{17, 6}},
				b:        &ds.Version{Components: []uint64{18, 0}},
				expected: -1,
			},
			{
				a:        &ds.Version{Components: []uint64{18, 0}},
				b:        &ds.Version{Components: []uint64{17, 6}},
				expected: 1,
			},
			{
				a:        &ds.Version{Components: []uint64{5, 0, 0}},
				b:        &ds.Version{Components: []uint64{5, 0, 0}},
				expected: 0,
			},
			{
				a:        &ds.Version{Components: []uint64{5, 0, 0}},
				b:        &ds.Version{Components: []uint64{5, 0, 1}},
				expected: -1,
			},
			{
				a:        &ds.Version{Components: []uint64{5, 0, 1}},
				b:        &ds.Version{Components: []uint64{5, 0, 0}},
				expected: 1,
			},
			{
				a:        &ds.Version{Components: []uint64{17}},
				b:        &ds.Version{Components: []uint64{17, 6}},
				expected: -1,
			},
			{
				// Even though these look equivalent, we'll consider the version
				// with more components to be greater than one with fewer.
				a:        &ds.Version{Components: []uint64{5}},
				b:        &ds.Version{Components: []uint64{5, 0, 0}},
				expected: -1,
			},
			{
				// Empty version is valid and considered less than all other
				// versions
				a:        &ds.Version{},
				b:        &ds.Version{Components: []uint64{1, 0, 0}},
				expected: -1,
			},
		} {
			t.Run(fmt.Sprintf("%s and %s", tc.a.String(), tc.b.String()), func(t *testing.T) {
				result := tc.a.Compare(tc.b)
				switch {
				case tc.expected == 0:
					assert.Zero(t, result)
				case tc.expected < 0:
					assert.Negative(t, result)
				default:
					assert.Positive(t, result)
				}
			})
		}
	})

	t.Run("json marshal and unmarshal", func(t *testing.T) {
		version := &ds.Version{Components: []uint64{17, 6}}

		raw, err := json.Marshal(version)
		assert.NoError(t, err)
		assert.Equal(t, `"17.6"`, string(raw))

		var out *ds.Version
		err = json.Unmarshal(raw, &out)
		assert.NoError(t, err)
		assert.Equal(t, version, out)
	})

	t.Run("backwards compatibility", func(t *testing.T) {
		// Make sure that we can unmarshal old values from storage
		var out *ds.Version
		err := json.Unmarshal([]byte(`{"semver": "17.0.0"}`), &out)
		assert.NoError(t, err)
		assert.Equal(t, &ds.Version{Components: []uint64{17, 0, 0}}, out)
	})
}

func TestNewPgEdgeVersion(t *testing.T) {
	for _, tc := range []struct {
		postgresVersion string
		spockVersion    string
		expected        *ds.PgEdgeVersion
		expectedErr     string
	}{
		{
			postgresVersion: "17.6",
			spockVersion:    "5.0.0",
			expected: &ds.PgEdgeVersion{
				PostgresVersion: &ds.Version{Components: []uint64{17, 6}},
				SpockVersion:    &ds.Version{Components: []uint64{5, 0, 0}},
			},
		},
		{
			postgresVersion: "invalid",
			spockVersion:    "5.0.0",
			expectedErr:     "invalid postgres version",
		},
		{
			postgresVersion: "17.6",
			spockVersion:    "invalid",
			expectedErr:     "invalid spock version",
		},
	} {
		t.Run(tc.postgresVersion+"_"+tc.spockVersion, func(t *testing.T) {
			result, err := ds.NewPgEdgeVersion(tc.postgresVersion, tc.spockVersion)
			if tc.expectedErr != "" {
				assert.Nil(t, result)
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}

func TestPgEdgeVersion(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		version := ds.MustPgEdgeVersion("17.6", "5.0.0")
		assert.Equal(t, "17.6_5.0.0", version.String())
	})

	t.Run("Compare", func(t *testing.T) {
		for _, tc := range []struct {
			a        *ds.PgEdgeVersion
			b        *ds.PgEdgeVersion
			expected int
		}{
			{
				a:        ds.MustPgEdgeVersion("17.6", "5.0.0"),
				b:        ds.MustPgEdgeVersion("17.6", "5.0.0"),
				expected: 0,
			},
			{
				a:        ds.MustPgEdgeVersion("18.0", "5.0.0"),
				b:        ds.MustPgEdgeVersion("17.6", "5.0.0"),
				expected: 1,
			},
			{
				a:        ds.MustPgEdgeVersion("17.6", "5.0.0"),
				b:        ds.MustPgEdgeVersion("18.0", "5.0.0"),
				expected: -1,
			},
			{
				a:        ds.MustPgEdgeVersion("17.6", "5.0.0"),
				b:        ds.MustPgEdgeVersion("17.6", "5.0.1"),
				expected: -1,
			},
			{
				a:        ds.MustPgEdgeVersion("17.6", "5.0.0"),
				b:        ds.MustPgEdgeVersion("17.6", "4.10.0"),
				expected: 1,
			},
		} {
			t.Run(fmt.Sprintf("%s and %s", tc.a, tc.b), func(t *testing.T) {
				result := tc.a.Compare(tc.b)
				switch {
				case tc.expected == 0:
					assert.Zero(t, result)
					assert.True(t, tc.a.Equals(tc.b))
				case tc.expected < 0:
					assert.Negative(t, result)
					assert.True(t, tc.a.LessThan(tc.b))
				default:
					assert.Positive(t, result)
					assert.True(t, tc.a.GreaterThan(tc.b))
				}
			})
		}
	})

	t.Run("json marshal and unmarshal", func(t *testing.T) {
		version := &ds.PgEdgeVersion{
			PostgresVersion: &ds.Version{Components: []uint64{17, 6}},
			SpockVersion:    &ds.Version{Components: []uint64{5}},
		}

		raw, err := json.Marshal(version)
		assert.NoError(t, err)
		assert.Equal(t, `{"postgres_version":"17.6","spock_version":"5"}`, string(raw))

		var out *ds.PgEdgeVersion
		err = json.Unmarshal(raw, &out)
		assert.NoError(t, err)
		assert.Equal(t, version, out)
	})

	t.Run("backwards compatibility", func(t *testing.T) {
		// Make sure that we can unmarshal old values from storage
		var out *ds.PgEdgeVersion
		err := json.Unmarshal([]byte(`{"postgres_version":{"semver":"17.0.0"},"spock_version":{"semver":"5.0.0"}}`), &out)
		assert.NoError(t, err)
		assert.Equal(t, &ds.PgEdgeVersion{
			PostgresVersion: &ds.Version{Components: []uint64{17, 0, 0}},
			SpockVersion:    &ds.Version{Components: []uint64{5, 0, 0}},
		}, out)
	})
}

func TestVersionConstraint_IsSatisfied(t *testing.T) {
	v := func(s string) *ds.Version {
		v, err := ds.ParseVersion(s)
		require.NoError(t, err)
		return v
	}

	for _, tc := range []struct {
		name       string
		constraint *ds.VersionConstraint
		version    *ds.Version
		expected   bool
	}{
		{
			name:       "nil min and max is always satisfied",
			constraint: &ds.VersionConstraint{},
			version:    v("5.0.0"),
			expected:   true,
		},
		{
			name:       "min only - satisfied",
			constraint: &ds.VersionConstraint{Min: v("16")},
			version:    v("17"),
			expected:   true,
		},
		{
			name:       "min only - exactly at min",
			constraint: &ds.VersionConstraint{Min: v("17")},
			version:    v("17"),
			expected:   true,
		},
		{
			name:       "min only - below min",
			constraint: &ds.VersionConstraint{Min: v("17")},
			version:    v("16"),
			expected:   false,
		},
		{
			name:       "max only - satisfied",
			constraint: &ds.VersionConstraint{Max: v("18")},
			version:    v("17"),
			expected:   true,
		},
		{
			name:       "max only - exactly at max",
			constraint: &ds.VersionConstraint{Max: v("17")},
			version:    v("17"),
			expected:   true,
		},
		{
			name:       "max only - above max",
			constraint: &ds.VersionConstraint{Max: v("17")},
			version:    v("18"),
			expected:   false,
		},
		{
			name:       "range - within bounds",
			constraint: &ds.VersionConstraint{Min: v("16"), Max: v("18")},
			version:    v("17"),
			expected:   true,
		},
		{
			name:       "range - at min boundary",
			constraint: &ds.VersionConstraint{Min: v("16"), Max: v("18")},
			version:    v("16"),
			expected:   true,
		},
		{
			name:       "range - at max boundary",
			constraint: &ds.VersionConstraint{Min: v("16"), Max: v("18")},
			version:    v("18"),
			expected:   true,
		},
		{
			name:       "range - below min",
			constraint: &ds.VersionConstraint{Min: v("16"), Max: v("18")},
			version:    v("15"),
			expected:   false,
		},
		{
			name:       "range - above max",
			constraint: &ds.VersionConstraint{Min: v("16"), Max: v("18")},
			version:    v("19"),
			expected:   false,
		},
		{
			name:       "semver min and max",
			constraint: &ds.VersionConstraint{Min: v("4.0.0"), Max: v("5.0.0")},
			version:    v("4.10.0"),
			expected:   true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.constraint.IsSatisfied(tc.version))
		})
	}
}

func TestVersionConstraint_String(t *testing.T) {
	v := func(s string) *ds.Version {
		v, err := ds.ParseVersion(s)
		require.NoError(t, err)
		return v
	}

	for _, tc := range []struct {
		name       string
		constraint *ds.VersionConstraint
		expected   string
	}{
		{
			name:       "no constraints",
			constraint: &ds.VersionConstraint{},
			expected:   "any",
		},
		{
			name:       "min only",
			constraint: &ds.VersionConstraint{Min: v("16")},
			expected:   ">= 16",
		},
		{
			name:       "max only",
			constraint: &ds.VersionConstraint{Max: v("18")},
			expected:   "<= 18",
		},
		{
			name:       "min and max",
			constraint: &ds.VersionConstraint{Min: v("16"), Max: v("18")},
			expected:   ">= 16 and <= 18",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.constraint.String())
		})
	}
}
