package swarm

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/config"
)

func TestGetServiceImage(t *testing.T) {
	cfg := config.Config{
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost: "ghcr.io/pgedge",
		},
	}
	sv := NewServiceVersions(cfg)

	tests := []struct {
		name        string
		serviceType string
		version     string
		want        string
		wantErr     bool
	}{
		{
			name:        "valid mcp 1.0.0",
			serviceType: "mcp",
			version:     "1.0.0",
			want:        "ghcr.io/pgedge/postgres-mcp:1.0.0",
			wantErr:     false,
		},
		{
			name:        "unsupported service type",
			serviceType: "unknown",
			version:     "1.0.0",
			want:        "",
			wantErr:     true,
		},
		{
			name:        "unsupported version",
			serviceType: "mcp",
			version:     "99.99.99",
			want:        "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sv.GetServiceImage(tt.serviceType, tt.version)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetServiceImage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetServiceImage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSupportedServiceVersions(t *testing.T) {
	cfg := config.Config{
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost: "ghcr.io/pgedge",
		},
	}
	sv := NewServiceVersions(cfg)

	tests := []struct {
		name        string
		serviceType string
		wantLen     int
		wantErr     bool
	}{
		{
			name:        "mcp service has versions",
			serviceType: "mcp",
			wantLen:     2, // "1.0.0" and "latest"
			wantErr:     false,
		},
		{
			name:        "unsupported service type",
			serviceType: "unknown",
			wantLen:     0,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sv.SupportedServiceVersions(tt.serviceType)
			if (err != nil) != tt.wantErr {
				t.Errorf("SupportedServiceVersions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != tt.wantLen {
				t.Errorf("SupportedServiceVersions() returned %d versions, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestServiceImageTag(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
		repoHost string
		want     string
	}{
		{
			name:     "image without registry",
			imageRef: "pgedge/postgres-mcp:1.0.0",
			repoHost: "ghcr.io/pgedge",
			want:     "ghcr.io/pgedge/pgedge/postgres-mcp:1.0.0",
		},
		{
			name:     "image with registry",
			imageRef: "docker.io/pgedge/postgres-mcp:1.0.0",
			repoHost: "ghcr.io/pgedge",
			want:     "docker.io/pgedge/postgres-mcp:1.0.0",
		},
		{
			name:     "image with localhost registry",
			imageRef: "localhost:5000/postgres-mcp:1.0.0",
			repoHost: "ghcr.io/pgedge",
			want:     "localhost:5000/postgres-mcp:1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				DockerSwarm: config.DockerSwarm{
					ImageRepositoryHost: tt.repoHost,
				},
			}
			got := serviceImageTag(cfg, tt.imageRef)
			if got != tt.want {
				t.Errorf("serviceImageTag() = %v, want %v", got, tt.want)
			}
		})
	}
}
