package swarm

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/host"
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
		wantTag     string
		wantErr     bool
	}{
		{
			name:        "valid mcp latest",
			serviceType: "mcp",
			version:     "latest",
			wantTag:     "ghcr.io/pgedge/postgres-mcp:latest",
			wantErr:     false,
		},
		{
			name:        "unsupported service type",
			serviceType: "unknown",
			version:     "latest",
			wantTag:     "",
			wantErr:     true,
		},
		{
			name:        "unregistered version",
			serviceType: "mcp",
			version:     "1.0.0",
			wantTag:     "",
			wantErr:     true,
		},
		{
			name:        "unsupported version",
			serviceType: "mcp",
			version:     "99.99.99",
			wantTag:     "",
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
			if tt.wantErr {
				if got != nil {
					t.Errorf("GetServiceImage() = %v, want nil", got)
				}
				return
			}
			if got.Tag != tt.wantTag {
				t.Errorf("GetServiceImage().Tag = %v, want %v", got.Tag, tt.wantTag)
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
			wantLen:     1, // "latest"
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
			name:     "bare image name",
			imageRef: "postgres-mcp:latest",
			repoHost: "ghcr.io/pgedge",
			want:     "ghcr.io/pgedge/postgres-mcp:latest",
		},
		{
			name:     "empty repository host",
			imageRef: "postgres-mcp:latest",
			repoHost: "",
			want:     "postgres-mcp:latest",
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

func TestGetServiceImage_ConstraintsPopulated(t *testing.T) {
	cfg := config.Config{
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost: "ghcr.io/pgedge",
		},
	}
	sv := NewServiceVersions(cfg)

	t.Run("mcp has no constraints", func(t *testing.T) {
		img, err := sv.GetServiceImage("mcp", "latest")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if img.PostgresConstraint != nil {
			t.Error("expected nil PostgresConstraint for mcp")
		}
		if img.SpockConstraint != nil {
			t.Error("expected nil SpockConstraint for mcp")
		}
	})

}

func mustVersion(t *testing.T, s string) *host.Version {
	t.Helper()
	v, err := host.ParseVersion(s)
	if err != nil {
		t.Fatalf("failed to parse version %q: %v", s, err)
	}
	return v
}

func TestValidateCompatibility(t *testing.T) {
	tests := []struct {
		name     string
		image    *ServiceImage
		postgres *host.Version
		spock    *host.Version
		wantErr  bool
	}{
		{
			name: "no constraints - always passes",
			image: &ServiceImage{
				Tag: "test:latest",
			},
			postgres: mustVersion(t, "17"),
			spock:    mustVersion(t, "5.0.0"),
			wantErr:  false,
		},
		{
			name: "postgres constraint satisfied",
			image: &ServiceImage{
				Tag:                "test:latest",
				PostgresConstraint: &host.VersionConstraint{Min: mustVersion(t, "16")},
			},
			postgres: mustVersion(t, "17"),
			spock:    mustVersion(t, "5.0.0"),
			wantErr:  false,
		},
		{
			name: "postgres constraint not satisfied",
			image: &ServiceImage{
				Tag:                "test:latest",
				PostgresConstraint: &host.VersionConstraint{Min: mustVersion(t, "18")},
			},
			postgres: mustVersion(t, "17"),
			spock:    mustVersion(t, "5.0.0"),
			wantErr:  true,
		},
		{
			name: "spock constraint not satisfied",
			image: &ServiceImage{
				Tag:             "test:latest",
				SpockConstraint: &host.VersionConstraint{Max: mustVersion(t, "4.0.0")},
			},
			postgres: mustVersion(t, "17"),
			spock:    mustVersion(t, "5.0.0"),
			wantErr:  true,
		},
		{
			name: "both constraints satisfied",
			image: &ServiceImage{
				Tag:                "test:latest",
				PostgresConstraint: &host.VersionConstraint{Min: mustVersion(t, "16"), Max: mustVersion(t, "18")},
				SpockConstraint:    &host.VersionConstraint{Min: mustVersion(t, "4.0.0"), Max: mustVersion(t, "6.0.0")},
			},
			postgres: mustVersion(t, "17"),
			spock:    mustVersion(t, "5.0.0"),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.image.ValidateCompatibility(tt.postgres, tt.spock)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCompatibility() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
