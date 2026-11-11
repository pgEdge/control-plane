package systemd

import (
	"context"
	"errors"
	"fmt"
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
	// postgrestUser is the Linux user that PostgREST units run as. The
	// pgedge-postgrest package creates it as the shared pgEdge services user.
	postgrestUser = "pgedge"
	// postgrestConfigBaseDir holds one config directory per service instance.
	postgrestConfigBaseDir = "/etc/postgrest"
	// postgrestListenHost makes PostgREST listen on all IPv4 and IPv6
	// addresses.
	postgrestListenHost = "*"
	// postgrestLimitNOFILE matches the upstream PostgREST systemd reference.
	postgrestLimitNOFILE = 10000
)

func PostgRESTUnitOptions(
	binPath string,
	configPath string,
	user string,
	group string,
	port int,
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
			ExecStart: ExecCommandLine(binPath, configPath),
			// SIGUSR2 reloads PostgREST's configuration file. SIGUSR1 would
			// only reload its schema cache.
			ExecReload:  "/bin/kill -USR2 $MAINPID",
			CPUs:        cpus,
			MemoryBytes: memoryBytes,
			Restart:     ServiceRestartOnFailure,
			LimitNOFILE: postgrestLimitNOFILE,
			// The listen address lives in the unit rather than postgrest.conf
			// so that the config file is identical across orchestrators.
			// Neither setting is reloadable, and changing the unit restarts
			// the service.
			Environment: map[string]string{
				"PGRST_SERVER_HOST": postgrestListenHost,
				"PGRST_SERVER_PORT": strconv.Itoa(port),
			},
		},
		Install: InstallSection{
			WantedBy: []string{"multi-user.target"},
		},
		Metadata: metadata,
	}.Options()
}

func (o *Orchestrator) generatePostgRESTInstanceResources(spec *database.ServiceInstanceSpec) (*database.ServiceInstanceResources, error) {
	postgrestConfig, errs := database.ParsePostgRESTServiceConfig(spec.ServiceSpec.Config)
	if len(errs) > 0 {
		return nil, fmt.Errorf("failed to parse PostgREST service config: %w", errors.Join(errs...))
	}

	port := utils.FromPointer(spec.Port)
	if port == 0 {
		return nil, fmt.Errorf("port is required for systemd service instances, missing for service instance '%s'", spec.ServiceInstanceID)
	}

	version, err := o.installedServiceVersion(spec.ServiceSpec)
	if err != nil {
		return nil, err
	}

	uid, gid, err := o.lookupServiceUser(postgrestUser, "pgedge-postgrest")
	if err != nil {
		return nil, err
	}

	databaseInstances, err := serviceDatabaseInstances(spec)
	if err != nil {
		return nil, err
	}

	preflight := &common.PostgRESTPreflightResource{
		ServiceID:    spec.ServiceSpec.ServiceID,
		DatabaseID:   spec.DatabaseID,
		DatabaseName: spec.DatabaseName,
		NodeName:     spec.NodeName,
		DBSchemas:    postgrestConfig.DBSchemas,
		DBAnonRole:   postgrestConfig.DBAnonRole,
	}
	configDir := &filesystem.DirResource{
		ID:       spec.ServiceInstanceID + "-data",
		HostID:   spec.HostID,
		Path:     filepath.Join(postgrestConfigBaseDir, spec.ServiceInstanceID),
		OwnerUID: uid,
		OwnerGID: gid,
	}
	config := &PostgRESTConfig{
		Base: &common.PostgRESTConfigResource{
			ServiceInstanceID:  spec.ServiceInstanceID,
			ServiceID:          spec.ServiceSpec.ServiceID,
			HostID:             spec.HostID,
			DirResourceID:      configDir.ID,
			Config:             postgrestConfig,
			ConnectAsUsername:  spec.ConnectAsUsername,
			ConnectAsPassword:  spec.ConnectAsPassword,
			DatabaseName:       spec.DatabaseName,
			TargetSessionAttrs: spec.TargetSessionAttrs,
			OwnerUID:           uid,
			OwnerGID:           gid,
		},
		DatabaseInstances: databaseInstances,
	}

	unitName := serviceUnitName(spec.ServiceSpec.ServiceType, spec.ServiceInstanceID)
	serviceUnit := &UnitResource{
		DatabaseID: spec.DatabaseID,
		HostID:     spec.HostID,
		Name:       unitName,
		Options: PostgRESTUnitOptions(
			o.cfg.SystemD.PostgRESTPath,
			filepath.Join(configDir.Path, "postgrest.conf"),
			strconv.Itoa(uid),
			strconv.Itoa(gid),
			port,
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
			config.Identifier(),
			// PostgREST fails to start until Patroni has bootstrapped the
			// database and the schemas and anonymous role exist.
			preflight.Identifier(),
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

	resources := []resource.Resource{preflight}
	// PostgREST connects as the connect_as user on every node, so each node
	// needs its own authenticator configuration.
	for _, nodeName := range serviceNodeNames(spec) {
		resources = append(resources, &common.PostgRESTAuthenticatorResource{
			ServiceID:         spec.ServiceSpec.ServiceID,
			DatabaseID:        spec.DatabaseID,
			DatabaseName:      spec.DatabaseName,
			NodeName:          nodeName,
			DBAnonRole:        postgrestConfig.DBAnonRole,
			ConnectAsUsername: spec.ConnectAsUsername,
		})
	}
	resources = append(resources, configDir, config, serviceUnit, serviceInstance)

	return buildServiceInstanceResources(spec, resources)
}

var _ resource.Resource = (*PostgRESTConfig)(nil)

const ResourceTypePostgRESTConfig resource.Type = "systemd.postgrest_config"

func PostgRESTConfigIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypePostgRESTConfig,
	}
}

// PostgRESTConfig wraps the common PostgREST config resource to resolve the
// database host list at apply time, since database instances on systemd hosts
// are reached at their host's peer address and allocated port.
type PostgRESTConfig struct {
	Base *common.PostgRESTConfigResource `json:"base"`
	// DatabaseInstances are the IDs of the database instances to connect to,
	// in connection order.
	DatabaseInstances []string `json:"database_instances"`
}

func (c *PostgRESTConfig) ResourceVersion() string {
	return "1"
}

func (c *PostgRESTConfig) DiffIgnore() []string {
	// The base's host list is resolved from DatabaseInstances at apply time.
	return []string{"/base/database_hosts"}
}

func (c *PostgRESTConfig) Executor() resource.Executor {
	return c.Base.Executor()
}

func (c *PostgRESTConfig) Identifier() resource.Identifier {
	return PostgRESTConfigIdentifier(c.Base.ServiceInstanceID)
}

func (c *PostgRESTConfig) Dependencies() []resource.Identifier {
	deps := c.Base.Dependencies()
	for _, id := range c.DatabaseInstances {
		deps = append(deps, database.InstanceResourceIdentifier(id))
	}
	return deps
}

func (c *PostgRESTConfig) TypeDependencies() []resource.Type {
	return nil
}

func (c *PostgRESTConfig) Refresh(ctx context.Context, rc *resource.Context) error {
	return c.Base.Refresh(ctx, rc)
}

func (c *PostgRESTConfig) Create(ctx context.Context, rc *resource.Context) error {
	if err := c.resolveDatabaseHosts(ctx, rc); err != nil {
		return err
	}
	return c.Base.Create(ctx, rc)
}

func (c *PostgRESTConfig) Update(ctx context.Context, rc *resource.Context) error {
	if err := c.resolveDatabaseHosts(ctx, rc); err != nil {
		return err
	}
	var previous *common.PostgRESTConfigResource
	var previousFailed bool
	if stored, ok := rc.State.Get(c.Identifier()); ok {
		prev, err := resource.ToResource[*PostgRESTConfig](stored)
		if err != nil {
			return fmt.Errorf("failed to decode previous PostgREST config resource: %w", err)
		}
		previous = prev.Base
		previousFailed = stored.Error != ""
	}
	return c.Base.UpdateFrom(ctx, rc, previous, previousFailed)
}

func (c *PostgRESTConfig) Delete(ctx context.Context, rc *resource.Context) error {
	return c.Base.Delete(ctx, rc)
}

func (c *PostgRESTConfig) resolveDatabaseHosts(ctx context.Context, rc *resource.Context) error {
	resolved, err := resolveServiceDatabaseHosts(ctx, rc, c.DatabaseInstances)
	if err != nil {
		return err
	}
	c.Base.DatabaseHosts = resolved
	return nil
}
