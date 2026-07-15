package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

func TestParseImageTag(t *testing.T) {
	cases := []struct {
		name      string
		image     string
		wantPg    string
		wantSpock string
		wantOk    bool
	}{
		{"standard tag", "ghcr.io/pgedge/pgedge-postgres:17.9-spock5.0.6-standard-2", "17.9", "5.0.6", true},
		{"two-digit minor", "ghcr.io/pgedge/pgedge-postgres:17.10-spock5.0.6-standard-1", "17.10", "5.0.6", true},
		{"two-digit patch", "ghcr.io/pgedge/pgedge-postgres:17.9-spock5.0.10-standard-1", "17.9", "5.0.10", true},
		{"pg18", "ghcr.io/pgedge/pgedge-postgres:18.3-spock5.0.6-standard-2", "18.3", "5.0.6", true},
		{"custom registry", "registry.example.com/postgres:17.9-spock5.0.6-standard-2", "17.9", "5.0.6", true},
		{"no build number", "ghcr.io/pgedge/pgedge-postgres:17.10-spock5.0.10-standard", "17.10", "5.0.10", true},
		{"major-only spock", "ghcr.io/pgedge/pgedge-postgres:17.10-spock5-standard", "17.10", "5", true},
		{"unrecognizable: major-only pg", "ghcr.io/pgedge/pgedge-postgres:17-spock5-standard", "", "", false},
		{"digest-pinned with tag", "ghcr.io/pgedge/pgedge-postgres:17.10-spock5.0.9-standard-1@sha256:abc123", "17.10", "5.0.9", true},
		{"unrecognizable: dev tag", "ghcr.io/pgedge/pgedge-postgres:my-custom-image", "", "", false},
		{"unrecognizable: latest", "ghcr.io/pgedge/pgedge-postgres:latest", "", "", false},
		{"unrecognizable: semver only", "ghcr.io/pgedge/pgedge-postgres:17.9", "", "", false},
		{"unrecognizable: missing spock", "ghcr.io/pgedge/pgedge-postgres:17.9-standard-2", "", "", false},
		{"unrecognizable: digest only, no tag", "ghcr.io/pgedge/pgedge-postgres@sha256:abc123", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pg, spock, ok := parseImageTag(tc.image)
			assert.Equal(t, tc.wantOk, ok)
			if tc.wantOk {
				require.NotNil(t, pg)
				require.NotNil(t, spock)
				assert.Equal(t, tc.wantPg, pg.String())
				assert.Equal(t, tc.wantSpock, spock.String())
			}
		})
	}
}

func TestVersionHasPrefix(t *testing.T) {
	cases := []struct {
		name    string
		tagVer  string
		specVer string
		want    bool
	}{
		{"exact match", "5.0.6", "5.0.6", true},
		{"spec is major only", "5.0.6", "5", true},
		{"spec is major.minor", "5.0.6", "5.0", true},
		{"tag shorter than spec", "5.0", "5.0.6", false},
		{"major mismatch", "4.0.6", "5", false},
		{"minor mismatch", "5.1.0", "5.0", false},
		{"patch mismatch with full spec", "5.0.7", "5.0.6", false},
		{"pg exact match", "17.9", "17.9", true},
		{"pg major only spec", "17.9", "17", true},
		{"pg major mismatch", "18.3", "17", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tagVer := ds.MustParseVersion(tc.tagVer)
			specVer := ds.MustParseVersion(tc.specVer)
			assert.Equal(t, tc.want, versionHasPrefix(tagVer, specVer))
		})
	}
}
