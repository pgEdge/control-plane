package swarm

import (
	"fmt"
	"strings"
	"testing"
)

func TestServiceInstanceName(t *testing.T) {
	tests := []struct {
		name        string
		serviceType string
		databaseID  string
		serviceID   string
		hostID      string
	}{
		{
			name:        "short host ID",
			serviceType: "mcp",
			databaseID:  "my-db",
			serviceID:   "mcp-server",
			hostID:      "host1",
		},
		{
			name:        "UUID host ID",
			serviceType: "mcp",
			databaseID:  "my-db",
			serviceID:   "mcp-server",
			hostID:      "dbf5779c-492a-11f0-b11a-1b8cb15693a8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ServiceInstanceName(tt.serviceType, tt.databaseID, tt.serviceID, tt.hostID)

			// Verify format: {serviceType}-{databaseID}-{serviceID}-{8charHash}
			prefix := fmt.Sprintf("%s-%s-%s-", tt.serviceType, tt.databaseID, tt.serviceID)
			if !strings.HasPrefix(got, prefix) {
				t.Errorf("ServiceInstanceName() = %q, want prefix %q", got, prefix)
			}

			// Verify the suffix is exactly 8 characters (base36 hash)
			suffix := strings.TrimPrefix(got, prefix)
			if len(suffix) != 8 {
				t.Errorf("ServiceInstanceName() hash suffix = %q (len %d), want 8 chars", suffix, len(suffix))
			}

			// Verify deterministic
			got2 := ServiceInstanceName(tt.serviceType, tt.databaseID, tt.serviceID, tt.hostID)
			if got != got2 {
				t.Errorf("ServiceInstanceName() not deterministic: %q != %q", got, got2)
			}
		})
	}

	// Verify different host IDs produce different names
	t.Run("different hosts produce different names", func(t *testing.T) {
		name1 := ServiceInstanceName("mcp", "db1", "svc1", "host-a")
		name2 := ServiceInstanceName("mcp", "db1", "svc1", "host-b")
		if name1 == name2 {
			t.Errorf("different host IDs should produce different names, both got %q", name1)
		}
	})
}
