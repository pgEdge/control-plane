package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// splitLibs splits a shared_preload_libraries value into a trimmed set.
func splitLibs(v string) map[string]bool {
	set := map[string]bool{}
	for lib := range strings.SplitSeq(v, ",") {
		if l := strings.TrimSpace(lib); l != "" {
			set[l] = true
		}
	}
	return set
}

// TestParametersColdFrontEnabled verifies the ColdFront boot GUCs (#6a/#6b) are
// applied when the flag is set: pg_duckdb + coldfront are appended to
// shared_preload_libraries (keeping the existing libs incl. spock) and the
// duckdb.* trio is set.
func TestParametersColdFrontEnabled(t *testing.T) {
	p := &PatroniConfigGenerator{
		ColdFrontEnabled: true,
		MemoryBytes:      8 << 30,
		CPUs:             4,
		ClusterSize:      3,
	}
	params := p.parameters()

	libs := splitLibs(params["shared_preload_libraries"].(string))
	// The default libs must survive, and the coldfront stack must be present.
	for _, want := range []string{"pg_stat_statements", "snowflake", "spock", "pg_duckdb", "coldfront"} {
		assert.Truef(t, libs[want], "shared_preload_libraries missing %q: %v",
			want, params["shared_preload_libraries"])
	}

	assert.Equal(t, "/usr/lib/pgedge/coldfront/duckdb-extensions", params["duckdb.extension_directory"])
	assert.Equal(t, "true", params["duckdb.allow_unsigned_extensions"])
	assert.Equal(t, "false", params["duckdb.autoinstall_known_extensions"])
}

// TestParametersColdFrontWinsOverUserOverride verifies the coldfront stack is
// appended on top of a user-supplied shared_preload_libraries override (which
// keeps spock per the API validation), rather than being clobbered by it.
func TestParametersColdFrontWinsOverUserOverride(t *testing.T) {
	p := &PatroniConfigGenerator{
		ColdFrontEnabled: true,
		MemoryBytes:      8 << 30,
		CPUs:             4,
		ClusterSize:      3,
		SpecParameters: map[string]any{
			"shared_preload_libraries": "spock,pg_cron",
		},
	}
	params := p.parameters()

	libs := splitLibs(params["shared_preload_libraries"].(string))
	for _, want := range []string{"spock", "pg_cron", "pg_duckdb", "coldfront"} {
		assert.Truef(t, libs[want], "shared_preload_libraries missing %q: %v",
			want, params["shared_preload_libraries"])
	}
}

// TestParametersColdFrontDisabled verifies a non-ColdFront database is
// untouched: no duckdb.* GUCs and the default shared_preload_libraries.
func TestParametersColdFrontDisabled(t *testing.T) {
	p := &PatroniConfigGenerator{
		ColdFrontEnabled: false,
		MemoryBytes:      8 << 30,
		CPUs:             4,
		ClusterSize:      3,
	}
	params := p.parameters()

	assert.NotContains(t, params, "duckdb.extension_directory")
	libs := splitLibs(params["shared_preload_libraries"].(string))
	assert.False(t, libs["pg_duckdb"], "pg_duckdb should not be present when ColdFront is disabled")
	assert.False(t, libs["coldfront"], "coldfront should not be present when ColdFront is disabled")
}
