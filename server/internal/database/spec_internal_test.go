package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionsFromImage(t *testing.T) {
	cases := []struct {
		name      string
		image     string
		wantPg    string
		wantSpock string
		wantOk    bool
	}{
		{"empty image", "", "", "", false},
		{"pgEdge tag", "127.0.0.1:5001/pgedge/pgedge-postgres:16.14-spock5.0.10-standard-1", "16.14", "5", true},
		{"pgEdge tag with digest", "ghcr.io/pgedge/pgedge-postgres:17.9-spock5.0.6-standard-2@sha256:abc123", "17.9", "5", true},
		{"unrecognized dev tag", "127.0.0.1:5001/pgedge/pgedge-postgres:my-dev-build", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pg, spock, ok := versionsFromImage(tc.image)
			assert.Equal(t, tc.wantOk, ok)
			if tc.wantOk {
				assert.Equal(t, tc.wantPg, pg)
				assert.Equal(t, tc.wantSpock, spock)
			}
		})
	}
}

func TestOrchestratorOptsSwarmImage(t *testing.T) {
	assert.Equal(t, "", (*OrchestratorOpts)(nil).swarmImage())
	assert.Equal(t, "", (&OrchestratorOpts{}).swarmImage())
	assert.Equal(t, "my-image", (&OrchestratorOpts{Swarm: &SwarmOpts{Image: "my-image"}}).swarmImage())
}
