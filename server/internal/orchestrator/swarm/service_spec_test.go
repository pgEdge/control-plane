package swarm

import (
	"fmt"
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
		checkLabels    func(t *testing.T, labels map[string]string)
		checkNetworks  func(t *testing.T, networks []swarm.NetworkAttachmentConfig)
		checkContainer func(t *testing.T, spec *swarm.ContainerSpec)
		checkPlacement func(t *testing.T, placement *swarm.Placement)
		checkResources func(t *testing.T, resources *swarm.ResourceRequirements)
		checkPorts     func(t *testing.T, ports []swarm.PortConfig)
	}{
		{
			name: "basic MCP service with bind mount and entrypoint",
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
				ServiceImage: &ServiceImage{
					Tag: "ghcr.io/pgedge/postgres-mcp:latest",
				},
				Credentials: &database.ServiceUser{
					Username: "svc_db1mcp",
					Password: "testpassword",
					Role:     "pgedge_application_read_only",
				},
				DatabaseNetworkID: "db1-database",
				Port:              intPtr(8080),
				DataPath:          "/var/lib/pgedge/services/db1-mcp-server-host1",
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
				if networks[0].Target != "bridge" {
					t.Errorf("first network = %v, want bridge", networks[0].Target)
				}
				if networks[1].Target != "db1-database" {
					t.Errorf("second network = %v, want db1-database", networks[1].Target)
				}
			},
			checkContainer: func(t *testing.T, spec *swarm.ContainerSpec) {
				// User should be mcpContainerUID
				if spec.User != fmt.Sprintf("%d", mcpContainerUID) {
					t.Errorf("User = %v, want %d", spec.User, mcpContainerUID)
				}
				// Command should override entrypoint
				if len(spec.Command) != 1 || spec.Command[0] != "/app/pgedge-postgres-mcp" {
					t.Errorf("Command = %v, want [/app/pgedge-postgres-mcp]", spec.Command)
				}
				// Args should pass config file path
				if len(spec.Args) != 2 || spec.Args[0] != "-config" || spec.Args[1] != "/app/data/config.yaml" {
					t.Errorf("Args = %v, want [-config /app/data/config.yaml]", spec.Args)
				}
				// Should have bind mount
				if len(spec.Mounts) != 1 {
					t.Fatalf("got %d mounts, want 1", len(spec.Mounts))
				}
				m := spec.Mounts[0]
				if m.Source != "/var/lib/pgedge/services/db1-mcp-server-host1" {
					t.Errorf("mount source = %v, want /var/lib/pgedge/services/db1-mcp-server-host1", m.Source)
				}
				if m.Target != "/app/data" {
					t.Errorf("mount target = %v, want /app/data", m.Target)
				}
				// No env vars for config (config is via file)
				if len(spec.Env) > 0 {
					t.Errorf("expected no env vars, got %d: %v", len(spec.Env), spec.Env)
				}
				// Healthcheck should be set
				if spec.Healthcheck == nil {
					t.Error("healthcheck is nil")
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
				ServiceImage: &ServiceImage{
					Tag: "ghcr.io/pgedge/postgres-mcp:latest",
				},
				DatabaseNetworkID: "db1-database",
				DataPath:          "/var/lib/pgedge/services/db1-mcp-server-host1",
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

			if tt.checkLabels != nil {
				tt.checkLabels(t, got.TaskTemplate.ContainerSpec.Labels)
			}
			if tt.checkNetworks != nil {
				tt.checkNetworks(t, got.TaskTemplate.Networks)
			}
			if tt.checkContainer != nil {
				tt.checkContainer(t, got.TaskTemplate.ContainerSpec)
			}
			if tt.checkPlacement != nil {
				tt.checkPlacement(t, got.TaskTemplate.Placement)
			}
			if tt.checkResources != nil {
				tt.checkResources(t, got.TaskTemplate.Resources)
			}
			if tt.checkPorts != nil {
				tt.checkPorts(t, got.EndpointSpec.Ports)
			}

			// Check image
			if got.TaskTemplate.ContainerSpec.Image != tt.opts.ServiceImage.Tag {
				t.Errorf("image = %v, want %v", got.TaskTemplate.ContainerSpec.Image, tt.opts.ServiceImage.Tag)
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
				return
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
