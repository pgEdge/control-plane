package database

import (
	"testing"
)

func TestGenerateServiceUsername(t *testing.T) {
	tests := []struct {
		name      string
		serviceID string
		hostID    string
		want      string
	}{
		{
			name:      "standard service instance",
			serviceID: "mcp-server",
			hostID:    "host1",
			want:      "svc_mcp-server_host1",
		},
		{
			name:      "multiple services on same database - service 1",
			serviceID: "appmcp-1",
			hostID:    "host1",
			want:      "svc_appmcp-1_host1",
		},
		{
			name:      "multiple services on same database - service 2",
			serviceID: "appmcp-2",
			hostID:    "host1",
			want:      "svc_appmcp-2_host1",
		},
		{
			name:      "service with multi-part service ID",
			serviceID: "my-mcp-service",
			hostID:    "host2",
			want:      "svc_my-mcp-service_host2",
		},
		{
			name:      "simple service and host IDs",
			serviceID: "mcp",
			hostID:    "n1",
			want:      "svc_mcp_n1",
		},
		{
			name:      "long service ID uses hash suffix",
			serviceID: "very-long-service-name-that-exceeds-postgres-limit-significantly",
			hostID:    "host1",
			want:      "svc_very-long-service-name-that-exceeds-postgres-limit_175de8cf",
		},
		{
			name:      "long names with shared prefix produce different usernames (case A)",
			serviceID: "very-long-service-name-that-exceeds-postgres-limit-AAA",
			hostID:    "host1",
			want:      "svc_very-long-service-name-that-exceeds-postgres-limit_860c8613",
		},
		{
			name:      "long names with shared prefix produce different usernames (case B)",
			serviceID: "very-long-service-name-that-exceeds-postgres-limit-BBB",
			hostID:    "host1",
			want:      "svc_very-long-service-name-that-exceeds-postgres-limit_c9cb0bb2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateServiceUsername(tt.serviceID, tt.hostID)
			if got != tt.want {
				t.Errorf("GenerateServiceUsername() = %v, want %v", got, tt.want)
			}
			if len(got) > 63 {
				t.Errorf("GenerateServiceUsername() length = %d, must be <= 63", len(got))
			}
		})
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

func TestGenerateServiceName(t *testing.T) {
	tests := []struct {
		name        string
		serviceType string
		databaseID  string
		serviceID   string
		hostID      string
		want        string
	}{
		{
			name:        "mcp service",
			serviceType: "mcp",
			databaseID:  "db1",
			serviceID:   "mcp-server",
			hostID:      "host1",
			want:        "mcp-db1-mcp-server-host1",
		},
		{
			name:        "simple identifiers",
			serviceType: "svc",
			databaseID:  "db",
			serviceID:   "s1",
			hostID:      "h1",
			want:        "svc-db-s1-h1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateServiceName(tt.serviceType, tt.databaseID, tt.serviceID, tt.hostID)
			if got != tt.want {
				t.Errorf("GenerateServiceName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateServiceHostname(t *testing.T) {
	tests := []struct {
		name      string
		serviceID string
		hostID    string
		want      string
	}{
		{
			name:      "standard service instance",
			serviceID: "mcp-server",
			hostID:    "host1",
			want:      "mcp-server-host1",
		},
		{
			name:      "simple identifiers",
			serviceID: "svc",
			hostID:    "h1",
			want:      "svc-h1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateServiceHostname(tt.serviceID, tt.hostID)
			if got != tt.want {
				t.Errorf("GenerateServiceHostname() = %v, want %v", got, tt.want)
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
