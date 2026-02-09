package swarm

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/pgEdge/control-plane/server/internal/database"
)

func TestServiceContainerSpec(t *testing.T) {
	cpus := 2.0
	memoryBytes := uint64(2147483648) // 2GB

	tests := []struct {
		name    string
		opts    *ServiceContainerSpecOptions
		wantErr bool
		// Validation functions
		checkLabels      func(t *testing.T, labels map[string]string)
		checkNetworks    func(t *testing.T, networks []swarm.NetworkAttachmentConfig)
		checkEnv         func(t *testing.T, env []string)
		checkPlacement   func(t *testing.T, placement *swarm.Placement)
		checkResources   func(t *testing.T, resources *swarm.ResourceRequirements)
		checkHealthcheck func(t *testing.T, healthcheck *swarm.ContainerSpec)
		checkPorts       func(t *testing.T, ports []swarm.PortConfig)
	}{
		{
			name: "basic MCP service",
			opts: &ServiceContainerSpecOptions{
				ServiceSpec: &database.ServiceSpec{
					ServiceID:   "mcp-server",
					ServiceType: "mcp",
					Version:     "1.0.0",
					Config: map[string]interface{}{
						"llm_provider":      "anthropic",
						"llm_model":         "claude-sonnet-4-5",
						"anthropic_api_key": "sk-ant-api03-test",
					},
				},
				ServiceInstanceID: "db1-mcp-server-host1",
				DatabaseID:        "db1",
				DatabaseName:      "testdb",
				HostID:            "host1",
				ServiceName:       "db1-mcp-server-host1",
				Hostname:          "mcp-server-host1",
				CohortMemberID:    "swarm-node-123",
				ServiceImages: &ServiceImages{
					Image: "ghcr.io/pgedge/postgres-mcp:latest",
				},
				Credentials: &database.ServiceUser{
					Username: "svc_db1mcp",
					Password: "testpassword",
					Role:     "pgedge_application_read_only",
				},
				DatabaseNetworkID: "db1-database",
				DatabaseHost:      "postgres-instance-1",
				DatabasePort:      5432,
				Port:              intPtr(8080),
			},
			wantErr: false,
			checkLabels: func(t *testing.T, labels map[string]string) {
				expectedLabels := map[string]string{
					"pgedge.component":           "service",
					"pgedge.service.instance.id": "db1-mcp-server-host1",
					"pgedge.service.id":          "mcp-server",
					"pgedge.database.id":         "db1",
					"pgedge.host.id":             "host1",
				}
				for k, v := range expectedLabels {
					if labels[k] != v {
						t.Errorf("label %s = %v, want %v", k, labels[k], v)
					}
				}
			},
			checkNetworks: func(t *testing.T, networks []swarm.NetworkAttachmentConfig) {
				if len(networks) != 2 {
					t.Errorf("got %d networks, want 2", len(networks))
					return
				}
				// First network should be bridge
				if networks[0].Target != "bridge" {
					t.Errorf("first network = %v, want bridge", networks[0].Target)
				}
				// Second network should be database overlay
				if networks[1].Target != "db1-database" {
					t.Errorf("second network = %v, want db1-database", networks[1].Target)
				}
			},
			checkEnv: func(t *testing.T, env []string) {
				expectedEnv := []string{
					"PGHOST=postgres-instance-1",
					"PGPORT=5432",
					"PGDATABASE=testdb",
					"PGSSLMODE=prefer",
					"PGEDGE_SERVICE_ID=mcp-server",
					"PGEDGE_DATABASE_ID=db1",
					"PGUSER=svc_db1mcp",
					"PGPASSWORD=testpassword",
					"PGEDGE_LLM_PROVIDER=anthropic",
					"PGEDGE_LLM_MODEL=claude-sonnet-4-5",
					"PGEDGE_ANTHROPIC_API_KEY=sk-ant-api03-test",
				}
				if len(env) != len(expectedEnv) {
					t.Errorf("got %d env vars, want %d", len(env), len(expectedEnv))
				}
				for _, e := range expectedEnv {
					found := false
					for _, got := range env {
						if got == e {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("missing env var: %s", e)
					}
				}
			},
			checkPlacement: func(t *testing.T, placement *swarm.Placement) {
				if len(placement.Constraints) != 1 {
					t.Errorf("got %d constraints, want 1", len(placement.Constraints))
				}
				if placement.Constraints[0] != "node.id==swarm-node-123" {
					t.Errorf("constraint = %v, want node.id==swarm-node-123", placement.Constraints[0])
				}
			},
			checkResources: func(t *testing.T, resources *swarm.ResourceRequirements) {
				if resources != nil {
					t.Errorf("expected no resource limits, got %+v", resources)
				}
			},
			checkHealthcheck: func(t *testing.T, containerSpec *swarm.ContainerSpec) {
				if containerSpec.Healthcheck == nil {
					t.Fatal("healthcheck is nil")
				}
				if len(containerSpec.Healthcheck.Test) == 0 {
					t.Error("healthcheck test is empty")
				}
			},
			checkPorts: func(t *testing.T, ports []swarm.PortConfig) {
				if len(ports) != 1 {
					t.Errorf("got %d ports, want 1", len(ports))
					return
				}
				if ports[0].TargetPort != 8080 {
					t.Errorf("target port = %d, want 8080", ports[0].TargetPort)
				}
				if ports[0].Protocol != swarm.PortConfigProtocolTCP {
					t.Errorf("protocol = %v, want TCP", ports[0].Protocol)
				}
			},
		},
		{
			name: "service with resource limits",
			opts: &ServiceContainerSpecOptions{
				ServiceSpec: &database.ServiceSpec{
					ServiceID:   "mcp-server",
					ServiceType: "mcp",
					Version:     "1.0.0",
					CPUs:        &cpus,
					MemoryBytes: &memoryBytes,
					Config: map[string]interface{}{
						"llm_provider":   "openai",
						"llm_model":      "gpt-4",
						"openai_api_key": "sk-test",
					},
				},
				ServiceInstanceID: "db1-mcp-server-host1",
				DatabaseID:        "db1",
				DatabaseName:      "testdb",
				HostID:            "host1",
				ServiceName:       "db1-mcp-server-host1",
				Hostname:          "mcp-server-host1",
				CohortMemberID:    "swarm-node-123",
				ServiceImages: &ServiceImages{
					Image: "ghcr.io/pgedge/postgres-mcp:latest",
				},
				DatabaseNetworkID: "db1-database",
				DatabaseHost:      "postgres-instance-1",
				DatabasePort:      5432,
			},
			wantErr: false,
			checkResources: func(t *testing.T, resources *swarm.ResourceRequirements) {
				if resources == nil {
					t.Fatal("expected resource limits, got nil")
				}
				if resources.Limits == nil {
					t.Fatal("expected limits, got nil")
				}
				expectedCPU := int64(2.0 * 1e9)
				if resources.Limits.NanoCPUs != expectedCPU {
					t.Errorf("NanoCPUs = %d, want %d", resources.Limits.NanoCPUs, expectedCPU)
				}
				expectedMem := int64(memoryBytes)
				if resources.Limits.MemoryBytes != expectedMem {
					t.Errorf("MemoryBytes = %d, want %d", resources.Limits.MemoryBytes, expectedMem)
				}
			},
		},
		{
			name: "service with OpenAI provider",
			opts: &ServiceContainerSpecOptions{
				ServiceSpec: &database.ServiceSpec{
					ServiceID:   "mcp-server",
					ServiceType: "mcp",
					Version:     "1.0.0",
					Config: map[string]interface{}{
						"llm_provider":   "openai",
						"llm_model":      "gpt-4",
						"openai_api_key": "sk-openai-test",
					},
				},
				ServiceInstanceID: "db1-mcp-server-host1",
				DatabaseID:        "db1",
				DatabaseName:      "testdb",
				HostID:            "host1",
				ServiceName:       "db1-mcp-server-host1",
				Hostname:          "mcp-server-host1",
				CohortMemberID:    "swarm-node-123",
				ServiceImages: &ServiceImages{
					Image: "ghcr.io/pgedge/postgres-mcp:latest",
				},
				DatabaseNetworkID: "db1-database",
				DatabaseHost:      "postgres-instance-1",
				DatabasePort:      5432,
			},
			wantErr: false,
			checkEnv: func(t *testing.T, env []string) {
				expectedEnv := []string{
					"PGEDGE_LLM_PROVIDER=openai",
					"PGEDGE_LLM_MODEL=gpt-4",
					"PGEDGE_OPENAI_API_KEY=sk-openai-test",
				}
				for _, e := range expectedEnv {
					found := false
					for _, got := range env {
						if got == e {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("missing env var: %s", e)
					}
				}
			},
		},
		{
			name: "service with Ollama provider",
			opts: &ServiceContainerSpecOptions{
				ServiceSpec: &database.ServiceSpec{
					ServiceID:   "mcp-server",
					ServiceType: "mcp",
					Version:     "1.0.0",
					Config: map[string]interface{}{
						"llm_provider": "ollama",
						"llm_model":    "llama2",
						"ollama_url":   "http://localhost:11434",
					},
				},
				ServiceInstanceID: "db1-mcp-server-host1",
				DatabaseID:        "db1",
				DatabaseName:      "testdb",
				HostID:            "host1",
				ServiceName:       "db1-mcp-server-host1",
				Hostname:          "mcp-server-host1",
				CohortMemberID:    "swarm-node-123",
				ServiceImages: &ServiceImages{
					Image: "ghcr.io/pgedge/postgres-mcp:latest",
				},
				DatabaseNetworkID: "db1-database",
				DatabaseHost:      "postgres-instance-1",
				DatabasePort:      5432,
			},
			wantErr: false,
			checkEnv: func(t *testing.T, env []string) {
				expectedEnv := []string{
					"PGEDGE_LLM_PROVIDER=ollama",
					"PGEDGE_LLM_MODEL=llama2",
					"PGEDGE_OLLAMA_URL=http://localhost:11434",
				}
				for _, e := range expectedEnv {
					found := false
					for _, got := range env {
						if got == e {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("missing env var: %s", e)
					}
				}
			},
		},
		{
			name: "service without credentials",
			opts: &ServiceContainerSpecOptions{
				ServiceSpec: &database.ServiceSpec{
					ServiceID:   "mcp-server",
					ServiceType: "mcp",
					Version:     "1.0.0",
					Config: map[string]interface{}{
						"llm_provider":      "anthropic",
						"llm_model":         "claude-sonnet-4-5",
						"anthropic_api_key": "sk-ant-test",
					},
				},
				ServiceInstanceID: "db1-mcp-server-host1",
				DatabaseID:        "db1",
				DatabaseName:      "testdb",
				HostID:            "host1",
				ServiceName:       "db1-mcp-server-host1",
				Hostname:          "mcp-server-host1",
				CohortMemberID:    "swarm-node-123",
				ServiceImages: &ServiceImages{
					Image: "ghcr.io/pgedge/postgres-mcp:latest",
				},
				Credentials:       nil, // No credentials
				DatabaseNetworkID: "db1-database",
				DatabaseHost:      "postgres-instance-1",
				DatabasePort:      5432,
			},
			wantErr: false,
			checkEnv: func(t *testing.T, env []string) {
				// Should not have PGUSER or PGPASSWORD
				for _, e := range env {
					if strings.HasPrefix(e, "PGUSER=") || strings.HasPrefix(e, "PGPASSWORD=") {
						t.Errorf("unexpected credential env var: %s", e)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ServiceContainerSpec(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("ServiceContainerSpec() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Run validation checks
			if tt.checkLabels != nil {
				tt.checkLabels(t, got.TaskTemplate.ContainerSpec.Labels)
			}
			if tt.checkNetworks != nil {
				tt.checkNetworks(t, got.TaskTemplate.Networks)
			}
			if tt.checkEnv != nil {
				tt.checkEnv(t, got.TaskTemplate.ContainerSpec.Env)
			}
			if tt.checkPlacement != nil {
				tt.checkPlacement(t, got.TaskTemplate.Placement)
			}
			if tt.checkResources != nil {
				tt.checkResources(t, got.TaskTemplate.Resources)
			}
			if tt.checkHealthcheck != nil {
				tt.checkHealthcheck(t, got.TaskTemplate.ContainerSpec)
			}
			if tt.checkPorts != nil {
				tt.checkPorts(t, got.EndpointSpec.Ports)
			}

			// Check image
			if got.TaskTemplate.ContainerSpec.Image != tt.opts.ServiceImages.Image {
				t.Errorf("image = %v, want %v", got.TaskTemplate.ContainerSpec.Image, tt.opts.ServiceImages.Image)
			}

			// Check service name
			if got.Name != tt.opts.ServiceName {
				t.Errorf("service name = %v, want %v", got.Name, tt.opts.ServiceName)
			}

			// Check hostname
			if got.TaskTemplate.ContainerSpec.Hostname != tt.opts.Hostname {
				t.Errorf("hostname = %v, want %v", got.TaskTemplate.ContainerSpec.Hostname, tt.opts.Hostname)
			}
		})
	}
}

func TestBuildServiceEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		opts     *ServiceContainerSpecOptions
		expected []string
	}{
		{
			name: "anthropic provider with credentials",
			opts: &ServiceContainerSpecOptions{
				ServiceSpec: &database.ServiceSpec{
					ServiceID: "mcp-server",
					Config: map[string]interface{}{
						"llm_provider":      "anthropic",
						"llm_model":         "claude-sonnet-4-5",
						"anthropic_api_key": "sk-ant-test",
					},
				},
				DatabaseID:   "db1",
				DatabaseName: "testdb",
				DatabaseHost: "postgres-instance-1",
				DatabasePort: 5432,
				Credentials: &database.ServiceUser{
					Username: "svc_test",
					Password: "testpass",
				},
			},
			expected: []string{
				"PGHOST=postgres-instance-1",
				"PGPORT=5432",
				"PGDATABASE=testdb",
				"PGSSLMODE=prefer",
				"PGEDGE_SERVICE_ID=mcp-server",
				"PGEDGE_DATABASE_ID=db1",
				"PGUSER=svc_test",
				"PGPASSWORD=testpass",
				"PGEDGE_LLM_PROVIDER=anthropic",
				"PGEDGE_LLM_MODEL=claude-sonnet-4-5",
				"PGEDGE_ANTHROPIC_API_KEY=sk-ant-test",
			},
		},
		{
			name: "openai provider without credentials",
			opts: &ServiceContainerSpecOptions{
				ServiceSpec: &database.ServiceSpec{
					ServiceID: "mcp-server",
					Config: map[string]interface{}{
						"llm_provider":   "openai",
						"llm_model":      "gpt-4",
						"openai_api_key": "sk-openai-test",
					},
				},
				DatabaseID:   "db1",
				DatabaseName: "testdb",
				DatabaseHost: "postgres-instance-1",
				DatabasePort: 5432,
				Credentials:  nil,
			},
			expected: []string{
				"PGHOST=postgres-instance-1",
				"PGPORT=5432",
				"PGDATABASE=testdb",
				"PGSSLMODE=prefer",
				"PGEDGE_SERVICE_ID=mcp-server",
				"PGEDGE_DATABASE_ID=db1",
				"PGEDGE_LLM_PROVIDER=openai",
				"PGEDGE_LLM_MODEL=gpt-4",
				"PGEDGE_OPENAI_API_KEY=sk-openai-test",
			},
		},
		{
			name: "ollama provider",
			opts: &ServiceContainerSpecOptions{
				ServiceSpec: &database.ServiceSpec{
					ServiceID: "mcp-server",
					Config: map[string]interface{}{
						"llm_provider": "ollama",
						"llm_model":    "llama2",
						"ollama_url":   "http://localhost:11434",
					},
				},
				DatabaseID:   "db1",
				DatabaseName: "testdb",
				DatabaseHost: "postgres-instance-1",
				DatabasePort: 5432,
			},
			expected: []string{
				"PGHOST=postgres-instance-1",
				"PGPORT=5432",
				"PGDATABASE=testdb",
				"PGSSLMODE=prefer",
				"PGEDGE_SERVICE_ID=mcp-server",
				"PGEDGE_DATABASE_ID=db1",
				"PGEDGE_LLM_PROVIDER=ollama",
				"PGEDGE_LLM_MODEL=llama2",
				"PGEDGE_OLLAMA_URL=http://localhost:11434",
			},
		},
		{
			name: "minimal config without LLM settings",
			opts: &ServiceContainerSpecOptions{
				ServiceSpec: &database.ServiceSpec{
					ServiceID: "mcp-server",
					Config:    map[string]interface{}{},
				},
				DatabaseID:   "db1",
				DatabaseName: "testdb",
				DatabaseHost: "postgres-instance-1",
				DatabasePort: 5432,
			},
			expected: []string{
				"PGHOST=postgres-instance-1",
				"PGPORT=5432",
				"PGDATABASE=testdb",
				"PGSSLMODE=prefer",
				"PGEDGE_SERVICE_ID=mcp-server",
				"PGEDGE_DATABASE_ID=db1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildServiceEnvVars(tt.opts)

			if len(got) != len(tt.expected) {
				t.Errorf("got %d env vars, want %d", len(got), len(tt.expected))
			}

			for _, e := range tt.expected {
				found := false
				for _, g := range got {
					if g == e {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("missing expected env var: %s", e)
				}
			}
		})
	}
}

func TestBuildServicePortConfig(t *testing.T) {
	tests := []struct {
		name              string
		port              *int
		wantPortCount     int
		wantPublishedPort uint32
	}{
		{
			name:          "nil port - no port published",
			port:          nil,
			wantPortCount: 0,
		},
		{
			name:              "port 0 - random port",
			port:              intPtr(0),
			wantPortCount:     1,
			wantPublishedPort: 0,
		},
		{
			name:              "port 8080 - specific port",
			port:              intPtr(8080),
			wantPortCount:     1,
			wantPublishedPort: 8080,
		},
		{
			name:              "port 9000 - specific port",
			port:              intPtr(9000),
			wantPortCount:     1,
			wantPublishedPort: 9000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ports := buildServicePortConfig(tt.port)

			if len(ports) != tt.wantPortCount {
				t.Fatalf("got %d ports, want %d", len(ports), tt.wantPortCount)
			}

			if tt.wantPortCount == 0 {
				return // No port config expected
			}

			port := ports[0]
			if port.Protocol != swarm.PortConfigProtocolTCP {
				t.Errorf("protocol = %v, want TCP", port.Protocol)
			}
			if port.TargetPort != 8080 {
				t.Errorf("target port = %d, want 8080", port.TargetPort)
			}
			if port.PublishedPort != tt.wantPublishedPort {
				t.Errorf("published port = %d, want %d", port.PublishedPort, tt.wantPublishedPort)
			}
			if port.PublishMode != swarm.PortConfigPublishModeHost {
				t.Errorf("publish mode = %v, want host", port.PublishMode)
			}
			if port.Name != "http" {
				t.Errorf("port name = %s, want 'http'", port.Name)
			}
		})
	}
}

func intPtr(i int) *int {
	return &i
}
