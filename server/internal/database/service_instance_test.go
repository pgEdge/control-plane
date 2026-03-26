package database

import (
	"testing"
)

func TestGenerateServiceUsername(t *testing.T) {
	tests := []struct {
		name      string
		serviceID string
		mode      string
		want      string
	}{
		{
			name:      "standard service instance ro",
			serviceID: "mcp-server",
			mode:      "ro",
			want:      "svc_mcp_server_ro",
		},
		{
			name:      "standard service instance rw",
			serviceID: "mcp-server",
			mode:      "rw",
			want:      "svc_mcp_server_rw",
		},
		{
			name:      "multiple services on same database - service 1",
			serviceID: "appmcp-1",
			mode:      "ro",
			want:      "svc_appmcp_1_ro",
		},
		{
			name:      "multiple services on same database - service 2",
			serviceID: "appmcp-2",
			mode:      "ro",
			want:      "svc_appmcp_2_ro",
		},
		{
			name:      "service with multi-part service ID",
			serviceID: "my-mcp-service",
			mode:      "ro",
			want:      "svc_my_mcp_service_ro",
		},
		{
			name:      "simple service ID",
			serviceID: "mcp",
			mode:      "ro",
			want:      "svc_mcp_ro",
		},
		{
			name:      "long service ID uses hash suffix",
			serviceID: "very-long-service-name-that-exceeds-postgres-limit-significantly",
			mode:      "ro",
			want:      "", // computed below
		},
		{
			name:      "long names with shared prefix produce different usernames (case A)",
			serviceID: "very-long-service-name-that-exceeds-postgres-limit-AAA",
			mode:      "ro",
			want:      "", // computed below
		},
		{
			name:      "long names with shared prefix produce different usernames (case B)",
			serviceID: "very-long-service-name-that-exceeds-postgres-limit-BBB",
			mode:      "ro",
			want:      "", // computed below
		},
		{
			name:      "long service ID with rw mode still fits",
			serviceID: "very-long-service-name-that-exceeds-postgres-limit-significantly",
			mode:      "rw",
			want:      "", // computed below, just check length
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateServiceUsername(tt.serviceID, tt.mode)
			if tt.want != "" {
				if got != tt.want {
					t.Errorf("GenerateServiceUsername() = %v, want %v", got, tt.want)
				}
			}
			if len(got) > 63 {
				t.Errorf("GenerateServiceUsername() length = %d, must be <= 63", len(got))
			}
			if len(got) < 4 || got[:4] != "svc_" {
				t.Errorf("GenerateServiceUsername() = %v, must start with svc_", got)
			}
		})
	}

	// Verify long names with shared prefix produce different usernames
	a := GenerateServiceUsername("very-long-service-name-that-exceeds-postgres-limit-AAA", "ro")
	b := GenerateServiceUsername("very-long-service-name-that-exceeds-postgres-limit-BBB", "ro")
	if a == b {
		t.Errorf("long names with shared prefix should produce different usernames, both got %v", a)
	}

	// Verify different modes produce different usernames for the same serviceID
	roUser := GenerateServiceUsername("mcp-server", "ro")
	rwUser := GenerateServiceUsername("mcp-server", "rw")
	if roUser == rwUser {
		t.Errorf("different modes should produce different usernames, both got %v", roUser)
	}
}

func TestGenerateServiceInstanceID(t *testing.T) {
	tests := []struct {
		name       string
		databaseID string
		serviceID  string
		hostID     string
		want       string
	}{
		{
			name:       "standard service instance",
			databaseID: "db1",
			serviceID:  "mcp-server",
			hostID:     "host1",
			want:       "db1-mcp-server-host1",
		},
		{
			name:       "multi-part identifiers",
			databaseID: "my-database",
			serviceID:  "my-service",
			hostID:     "my-host",
			want:       "my-database-my-service-my-host",
		},
		{
			name:       "simple identifiers",
			databaseID: "db",
			serviceID:  "svc",
			hostID:     "h1",
			want:       "db-svc-h1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateServiceInstanceID(tt.databaseID, tt.serviceID, tt.hostID)
			if got != tt.want {
				t.Errorf("GenerateServiceInstanceID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateDatabaseNetworkID(t *testing.T) {
	tests := []struct {
		name       string
		databaseID string
		want       string
	}{
		{
			name:       "standard database",
			databaseID: "db1",
			want:       "db1",
		},
		{
			name:       "multi-part database ID",
			databaseID: "my-database",
			want:       "my-database",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateDatabaseNetworkID(tt.databaseID)
			if got != tt.want {
				t.Errorf("GenerateDatabaseNetworkID() = %v, want %v", got, tt.want)
			}
		})
	}
}
