package systemd

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/stretchr/testify/assert"
)

func TestDefaultVersion(t *testing.T) {
	pg18RC := &ds.PgEdgeVersion{
		PostgresVersion: ds.MustParseVersion("18.0"),
		SpockVersion:    ds.MustParseVersion("5"),
		Stability:       ds.StabilityRC,
	}
	pg17 := &ds.PgEdgeVersion{
		PostgresVersion: ds.MustParseVersion("17.11"),
		SpockVersion:    ds.MustParseVersion("5"),
	}
	pg16 := &ds.PgEdgeVersion{
		PostgresVersion: ds.MustParseVersion("16.15"),
		SpockVersion:    ds.MustParseVersion("5"),
	}
	pg16Beta := &ds.PgEdgeVersion{
		PostgresVersion: ds.MustParseVersion("16.15"),
		SpockVersion:    ds.MustParseVersion("6"),
		Stability:       ds.StabilityBeta,
	}

	for _, tc := range []struct {
		name     string
		versions []*ds.PgEdgeVersion
		expected *ds.PgEdgeVersion
	}{
		{
			name:     "single stable version",
			versions: []*ds.PgEdgeVersion{pg17},
			expected: pg17,
		},
		{
			name:     "multiple stable versions",
			versions: []*ds.PgEdgeVersion{pg17, pg16},
			expected: pg17,
		},
		{
			name:     "skips pre-releases",
			versions: []*ds.PgEdgeVersion{pg18RC, pg17, pg16},
			expected: pg17,
		},
		{
			name:     "only pre-releases",
			versions: []*ds.PgEdgeVersion{pg18RC, pg16Beta},
			expected: pg18RC,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Same(t, tc.expected, defaultVersion(tc.versions))
		})
	}
}
