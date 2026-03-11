package database

import (
	"testing"
)

func TestGenerateServiceUsername(t *testing.T) {
	tests := []struct {
		name      string
		serviceID string
		want      string
	}{
		{
			name:      "standard service instance",
			serviceID: "mcp-server",
			want:      "svc_mcp_server",
		},
		{
			name:      "multiple services on same database - service 1",
			serviceID: "appmcp-1",
			want:      "svc_appmcp_1",
		},
		{
			name:      "multiple services on same database - service 2",
			serviceID: "appmcp-2",
			want:      "svc_appmcp_2",
		},
		{
			name:      "service with multi-part service ID",
			serviceID: "my-mcp-service",
			want:      "svc_my_mcp_service",
		},
		{
			name:      "simple service ID",
			serviceID: "mcp",
			want:      "svc_mcp",
		},
		{
			name:      "long service ID uses hash suffix",
			serviceID: "very-long-service-name-that-exceeds-postgres-limit-significantly",
			want:      "", // computed below
		},
		{
			name:      "long names with shared prefix produce different usernames (case A)",
			serviceID: "very-long-service-name-that-exceeds-postgres-limit-AAA",
			want:      "", // computed below
		},
		{
			name:      "long names with shared prefix produce different usernames (case B)",
			serviceID: "very-long-service-name-that-exceeds-postgres-limit-BBB",
			want:      "", // computed below
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateServiceUsername(tt.serviceID)
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
	a := GenerateServiceUsername("very-long-service-name-that-exceeds-postgres-limit-AAA")
	b := GenerateServiceUsername("very-long-service-name-that-exceeds-postgres-limit-BBB")
	if a == b {
		t.Errorf("long names with shared prefix should produce different usernames, both got %v", a)
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
