package swarm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

func TestServiceInstanceName(t *testing.T) {
	tests := []struct {
		name       string
		databaseID string
		serviceID  string
		hostID     string
	}{
		{
			name:       "short IDs",
			databaseID: "my-db",
			serviceID:  "mcp-server",
			hostID:     "host1",
		},
		{
			name:       "UUID host ID",
			databaseID: "my-db",
			serviceID:  "mcp-server",
			hostID:     "dbf5779c-492a-11f0-b11a-1b8cb15693a8",
		},
		{
			name:       "postgrest service",
			databaseID: "storefront",
			serviceID:  "api",
			hostID:     "host-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ServiceInstanceName(tt.databaseID, tt.serviceID, tt.hostID)

			// Verify format: {databaseID}-{serviceID}-{8charHash}
			prefix := fmt.Sprintf("%s-%s-", tt.databaseID, tt.serviceID)
			if !strings.HasPrefix(got, prefix) {
				t.Errorf("ServiceInstanceName() = %q, want prefix %q", got, prefix)
			}

			// Verify the suffix is exactly 8 characters (base36 hash)
			suffix := strings.TrimPrefix(got, prefix)
			if len(suffix) != 8 {
				t.Errorf("ServiceInstanceName() hash suffix = %q (len %d), want 8 chars", suffix, len(suffix))
			}

			// Must be within Docker Swarm's 63-char limit.
			if len(got) > 63 {
				t.Errorf("ServiceInstanceName() = %q (len %d), must be <= 63 chars", got, len(got))
			}

			// Must be deterministic.
			got2 := ServiceInstanceName(tt.databaseID, tt.serviceID, tt.hostID)
			if got != got2 {
				t.Errorf("ServiceInstanceName() not deterministic: %q != %q", got, got2)
			}

			t.Logf("ServiceInstanceName() = %q (len %d)", got, len(got))
		})
	}

	t.Run("different hosts produce different names", func(t *testing.T) {
		name1 := ServiceInstanceName("db1", "svc1", "host-a")
		name2 := ServiceInstanceName("db1", "svc1", "host-b")
		if name1 == name2 {
			t.Errorf("different host IDs should produce different names, both got %q", name1)
		}
	})

	t.Run("different databases produce different names", func(t *testing.T) {
		name1 := ServiceInstanceName("db-aaa", "api", "host-1")
		name2 := ServiceInstanceName("db-bbb", "api", "host-1")
		if name1 == name2 {
			t.Errorf("different database IDs should produce different names, both got %q", name1)
		}
	})

	t.Run("different service IDs produce different names", func(t *testing.T) {
		name1 := ServiceInstanceName("db1", "api-v1", "host-1")
		name2 := ServiceInstanceName("db1", "api-v2", "host-1")
		if name1 == name2 {
			t.Errorf("different service IDs should produce different names, both got %q", name1)
		}
	})

}

func TestDefaultGUCs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		version  *ds.PgEdgeVersion
		expected map[string]any
	}{
		{
			name:    "nil version",
			version: nil,
			expected: map[string]any{
				"shared_preload_libraries": "pg_stat_statements,snowflake,spock",
			},
		},
		{
			name:    "with spock",
			version: ds.MustParsePgEdgeVersion("17.10", "5"),
			expected: map[string]any{
				"shared_preload_libraries": "pg_stat_statements,snowflake,spock",
			},
		},
		{
			name:    "with spock and output plugin libraries",
			version: ds.MustParsePgEdgeVersion("17.11", "5"),
			expected: map[string]any{
				"shared_preload_libraries": "pg_stat_statements,snowflake,spock",
				"output_plugin_libraries":  "pgoutput, test_decoding, spock_output",
			},
		},
		{
			name:    "without spock",
			version: ds.MustParsePgEdgeVersion("17.11", ""),
			expected: map[string]any{
				"shared_preload_libraries": "pg_stat_statements",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, defaultGUCs(tc.version))
		})
	}
}
