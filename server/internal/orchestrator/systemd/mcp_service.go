package systemd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"

	"github.com/coreos/go-systemd/v22/unit"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

const (
	// mcpPackageName is the package that installs the MCP server.
	mcpPackageName = "pgedge-postgres-mcp"
	// mcpUser is the Linux user that MCP units run as. The
	// pgedge-postgres-mcp package creates it as the shared pgEdge services
	// user.
	mcpUser = "pgedge"
	// mcpConfigBaseDir holds one config directory per service instance.
	mcpConfigBaseDir = "/etc/pgedge-mcp"
	// mcpDataBaseDir holds one data directory per service instance, for the
	// files that the server writes: its token and user stores, conversation
	// history and audit trace. The package creates the parent directory.
	mcpDataBaseDir = "/var/lib/pgedge/postgres-mcp"
	// mcpLimitNOFILE matches the unit that the package ships.
	mcpLimitNOFILE = 65536
)

func MCPUnitOptions(
	binPath string,
	configPath string,
	user string,
	group string,
	cpus float64,
	memoryBytes uint64,
	metadata map[string]string,
) []*unit.UnitOption {
	return UnitFile{
		Unit: UnitSection{
			After: []string{"network.target"},
		},
		Service: ServiceSection{
			Type:      ServiceTypeSimple,
			User:      user,
			Group:     group,
			ExecStart: ExecCommandLine(binPath, "-config", configPath),
			// SIGHUP makes the server re-read config.yaml and apply its
			// database connections. Other settings need a restart.
			ExecReload:  "/bin/kill -HUP $MAINPID",
			CPUs:        cpus,
			MemoryBytes: memoryBytes,
			Restart:     ServiceRestartOnFailure,
			LimitNOFILE: mcpLimitNOFILE,
		},
		Install: InstallSection{
			WantedBy: []string{"multi-user.target"},
		},
		Metadata: metadata,
	}.Options()
}

func (o *Orchestrator) generateMCPInstanceResources(spec *database.ServiceInstanceSpec) (*database.ServiceInstanceResources, error) {
	mcpConfig, errs := database.ParseMCPServiceConfig(spec.ServiceSpec.Config, false)
	if len(errs) > 0 {
		return nil, fmt.Errorf("failed to parse MCP service config: %w", errors.Join(errs...))
	}

	port := utils.FromPointer(spec.Port)
	if port == 0 {
		return nil, fmt.Errorf("port is required for systemd service instances, missing for service instance '%s'", spec.ServiceInstanceID)
	}

	version, err := o.installedServiceVersion(spec.ServiceSpec)
	if err != nil {
		return nil, err
	}

	uid, gid, err := o.lookupServiceUser(mcpUser, mcpPackageName)
	if err != nil {
		return nil, err
	}

	databaseInstances, err := serviceDatabaseInstances(spec)
	if err != nil {
		return nil, err
	}

	configDir := &filesystem.DirResource{
		ID:       spec.ServiceInstanceID + "-config",
		HostID:   spec.HostID,
		Path:     filepath.Join(mcpConfigBaseDir, spec.ServiceInstanceID),
		OwnerUID: uid,
		OwnerGID: gid,
	}
	dataDir := &filesystem.DirResource{
		ID:       spec.ServiceInstanceID + "-data",
		HostID:   spec.HostID,
		Path:     filepath.Join(mcpDataBaseDir, spec.ServiceInstanceID),
		OwnerUID: uid,
		OwnerGID: gid,
	}

	// There's no container, so the server reads and writes its files at
	// their host paths.
	paths := common.MCPConfigPaths{
		HTTPAddress: net.JoinHostPort("", strconv.Itoa(port)),
		TokenFile:   filepath.Join(dataDir.Path, "tokens.yaml"),
		UserFile:    filepath.Join(dataDir.Path, "users.yaml"),
		TraceFile:   filepath.Join(dataDir.Path, "audit-trace.log"),
		DataDir:     dataDir.Path,
	}
	var kbHostPath string
	if mcpConfig.KBEnabled != nil && *mcpConfig.KBEnabled {
		kbHostPath = filepath.Join(o.cfg.DataDir, "kb", "nla-kb.db")
		if mcpConfig.KBDatabaseHostPath != nil {
			kbHostPath = *mcpConfig.KBDatabaseHostPath
		}
		paths.KBDatabasePath = kbHostPath
	}

	config := &MCPConfig{
		Base: &common.MCPConfigResource{
			ServiceInstanceID:   spec.ServiceInstanceID,
			ServiceID:           spec.ServiceSpec.ServiceID,
			HostID:              spec.HostID,
			ConfigDirResourceID: configDir.ID,
			DataDirResourceID:   dataDir.ID,
			Config:              mcpConfig,
			DatabaseName:        spec.DatabaseName,
			TargetSessionAttrs:  spec.TargetSessionAttrs,
			ConnectAsUsername:   spec.ConnectAsUsername,
			ConnectAsPassword:   spec.ConnectAsPassword,
			Paths:               paths,
			KBHostPath:          kbHostPath,
			OwnerUID:            uid,
			OwnerGID:            gid,
		},
		DatabaseInstances: databaseInstances,
	}

	unitName := serviceUnitName(spec.ServiceSpec.ServiceType, spec.ServiceInstanceID)
	serviceUnit := &UnitResource{
		DatabaseID: spec.DatabaseID,
		HostID:     spec.HostID,
		Name:       unitName,
		Options: MCPUnitOptions(
			o.cfg.SystemD.MCPPath,
			filepath.Join(configDir.Path, "config.yaml"),
			strconv.Itoa(uid),
			strconv.Itoa(gid),
			utils.FromPointer(spec.ServiceSpec.CPUs),
			utils.FromPointer(spec.ServiceSpec.MemoryBytes),
			map[string]string{
				serviceMetadataServiceType:       spec.ServiceSpec.ServiceType,
				serviceMetadataServiceInstanceID: spec.ServiceInstanceID,
				serviceMetadataPort:              strconv.Itoa(port),
				serviceMetadataVersion:           version,
			},
		),
		ExtraDependencies: []resource.Identifier{
			configDir.Identifier(),
			dataDir.Identifier(),
			config.Identifier(),
		},
		RestartOnChange: true,
	}
	serviceInstance := &ServiceInstanceResource{
		ServiceInstanceID: spec.ServiceInstanceID,
		ServiceID:         spec.ServiceSpec.ServiceID,
		DatabaseID:        spec.DatabaseID,
		HostID:            spec.HostID,
		UnitName:          unitName,
	}

	return buildServiceInstanceResources(spec, []resource.Resource{
		configDir,
		dataDir,
		config,
		serviceUnit,
		serviceInstance,
	})
}

var _ resource.Resource = (*MCPConfig)(nil)

const ResourceTypeMCPConfig resource.Type = "systemd.mcp_config"

func MCPConfigIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeMCPConfig,
	}
}

// MCPConfig wraps the common MCP config resource to resolve the database host
// list at apply time, since database instances on systemd hosts are reached
// at their host's peer address and allocated port.
type MCPConfig struct {
	Base *common.MCPConfigResource `json:"base"`
	// DatabaseInstances are the IDs of the database instances to connect to,
	// in connection order.
	DatabaseInstances []string `json:"database_instances"`
}

func (c *MCPConfig) ResourceVersion() string {
	return "1"
}

func (c *MCPConfig) DiffIgnore() []string {
	// The base's host list is resolved from DatabaseInstances at apply time.
	return []string{"/base/database_hosts"}
}

func (c *MCPConfig) Executor() resource.Executor {
	return c.Base.Executor()
}

func (c *MCPConfig) Identifier() resource.Identifier {
	return MCPConfigIdentifier(c.Base.ServiceInstanceID)
}

func (c *MCPConfig) Dependencies() []resource.Identifier {
	deps := c.Base.Dependencies()
	for _, id := range c.DatabaseInstances {
		deps = append(deps, database.InstanceResourceIdentifier(id))
	}
	return deps
}

func (c *MCPConfig) TypeDependencies() []resource.Type {
	return nil
}

func (c *MCPConfig) Refresh(ctx context.Context, rc *resource.Context) error {
	return c.Base.Refresh(ctx, rc)
}

func (c *MCPConfig) Create(ctx context.Context, rc *resource.Context) error {
	if err := c.resolveDatabaseHosts(ctx, rc); err != nil {
		return err
	}
	return c.Base.Create(ctx, rc)
}

func (c *MCPConfig) Update(ctx context.Context, rc *resource.Context) error {
	if err := c.resolveDatabaseHosts(ctx, rc); err != nil {
		return err
	}
	var previous *common.MCPConfigResource
	var previousFailed bool
	if stored, ok := rc.State.Get(c.Identifier()); ok {
		prev, err := resource.ToResource[*MCPConfig](stored)
		if err != nil {
			return fmt.Errorf("failed to decode previous MCP config resource: %w", err)
		}
		previous = prev.Base
		previousFailed = stored.Error != ""
	}
	return c.Base.UpdateFrom(ctx, rc, previous, previousFailed)
}

func (c *MCPConfig) Delete(ctx context.Context, rc *resource.Context) error {
	return c.Base.Delete(ctx, rc)
}

func (c *MCPConfig) resolveDatabaseHosts(ctx context.Context, rc *resource.Context) error {
	resolved, err := resolveServiceDatabaseHosts(ctx, rc, c.DatabaseInstances)
	if err != nil {
		return err
	}
	c.Base.DatabaseHosts = resolved
	return nil
}
