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
	// ragPackageName is the package that installs the default RAG server.
	ragPackageName = "pgedge-rag-server2"
	// ragUser is the Linux user that RAG units run as. The RAG server
	// packages create it as the shared pgEdge services user.
	ragUser = "pgedge"
	// ragConfigBaseDir holds one directory per service instance, for its
	// config file and API key files. The server doesn't write any files.
	ragConfigBaseDir = "/etc/pgedge-rag"
	// ragListenAddress makes the server listen on all interfaces, like the
	// other service types on systemd hosts.
	ragListenAddress = "0.0.0.0"
)

// RAGUnitOptions returns the unit options for a RAG service instance. There's
// no ExecReload: the Control Plane restarts the unit to apply config changes.
func RAGUnitOptions(
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
			Type:        ServiceTypeSimple,
			User:        user,
			Group:       group,
			ExecStart:   ExecCommandLine(binPath, "-config", configPath),
			CPUs:        cpus,
			MemoryBytes: memoryBytes,
			Restart:     ServiceRestartOnFailure,
		},
		Install: InstallSection{
			WantedBy: []string{"multi-user.target"},
		},
		Metadata: metadata,
	}.Options()
}

func (o *Orchestrator) generateRAGInstanceResources(spec *database.ServiceInstanceSpec) (*database.ServiceInstanceResources, error) {
	ragConfig, errs := database.ParseRAGServiceConfig(spec.ServiceSpec.Config, false)
	if len(errs) > 0 {
		return nil, fmt.Errorf("failed to parse RAG service config: %w", errors.Join(errs...))
	}

	port := utils.FromPointer(spec.Port)
	if port == 0 {
		return nil, fmt.Errorf("port is required for systemd service instances, missing for service instance '%s'", spec.ServiceInstanceID)
	}

	version, err := o.installedServiceVersion(spec.ServiceSpec)
	if err != nil {
		return nil, err
	}

	uid, gid, err := o.lookupServiceUser(ragUser, ragPackageName)
	if err != nil {
		return nil, err
	}

	// Like on swarm, the server connects to the first database host, which
	// is the preferred one for this service instance.
	databaseInstances, err := serviceDatabaseInstances(spec)
	if err != nil {
		return nil, err
	}
	if len(databaseInstances) == 0 {
		return nil, fmt.Errorf("no database hosts for service instance '%s'", spec.ServiceInstanceID)
	}

	configDir := &filesystem.DirResource{
		ID:       spec.ServiceInstanceID + "-config",
		HostID:   spec.HostID,
		Path:     filepath.Join(ragConfigBaseDir, spec.ServiceInstanceID),
		OwnerUID: uid,
		OwnerGID: gid,
	}
	keys := &common.RAGServiceKeysResource{
		ServiceInstanceID: spec.ServiceInstanceID,
		HostID:            spec.HostID,
		ParentID:          configDir.ID,
		Keys:              common.ExtractRAGAPIKeys(ragConfig),
		OwnerUID:          uid,
		OwnerGID:          gid,
	}
	preflight := &common.RAGPreflightResource{
		ServiceInstanceID: spec.ServiceInstanceID,
		NodeName:          spec.NodeName,
		DatabaseName:      spec.DatabaseName,
		ConnectAsUsername: spec.ConnectAsUsername,
	}
	config := &RAGConfig{
		Base: &common.RAGConfigResource{
			ServiceInstanceID: spec.ServiceInstanceID,
			ServiceID:         spec.ServiceSpec.ServiceID,
			HostID:            spec.HostID,
			DirResourceID:     configDir.ID,
			Config:            ragConfig,
			DatabaseName:      spec.DatabaseName,
			ConnectAsUsername: spec.ConnectAsUsername,
			ConnectAsPassword: spec.ConnectAsPassword,
			// There's no container, so the server reads the key files at
			// their host paths.
			KeysDir:       filepath.Join(configDir.Path, common.RAGKeysDirName),
			ListenAddress: ragListenAddress,
			Port:          port,
			OwnerUID:      uid,
			OwnerGID:      gid,
		},
		DatabaseInstanceID: databaseInstances[0],
	}

	unitName := serviceUnitName(spec.ServiceSpec.ServiceType, spec.ServiceInstanceID)
	serviceUnit := &UnitResource{
		DatabaseID: spec.DatabaseID,
		HostID:     spec.HostID,
		Name:       unitName,
		Options: RAGUnitOptions(
			o.cfg.SystemD.RAGPath,
			filepath.Join(configDir.Path, common.RAGConfigFileName),
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
			keys.Identifier(),
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
		keys,
		preflight,
		config,
		serviceUnit,
		serviceInstance,
	})
}

var _ resource.Resource = (*RAGConfig)(nil)

const ResourceTypeRAGConfig resource.Type = "systemd.rag_config"

func RAGConfigIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeRAGConfig,
	}
}

// RAGConfig wraps the common RAG config resource to resolve the database host
// at apply time, since database instances on systemd hosts are reached at
// their host's peer address and allocated port.
type RAGConfig struct {
	Base *common.RAGConfigResource `json:"base"`
	// DatabaseInstanceID is the ID of the database instance to connect to.
	DatabaseInstanceID string `json:"database_instance_id"`
}

func (c *RAGConfig) ResourceVersion() string {
	return "1"
}

func (c *RAGConfig) DiffIgnore() []string {
	// The base's host and port are resolved from DatabaseInstanceID at apply
	// time.
	return []string{"/base/database_host", "/base/database_port"}
}

func (c *RAGConfig) Executor() resource.Executor {
	return c.Base.Executor()
}

func (c *RAGConfig) Identifier() resource.Identifier {
	return RAGConfigIdentifier(c.Base.ServiceInstanceID)
}

func (c *RAGConfig) Dependencies() []resource.Identifier {
	return append(c.Base.Dependencies(), database.InstanceResourceIdentifier(c.DatabaseInstanceID))
}

func (c *RAGConfig) TypeDependencies() []resource.Type {
	return nil
}

func (c *RAGConfig) Refresh(ctx context.Context, rc *resource.Context) error {
	return c.Base.Refresh(ctx, rc)
}

func (c *RAGConfig) Create(ctx context.Context, rc *resource.Context) error {
	if err := c.resolveDatabaseHost(ctx, rc); err != nil {
		return err
	}
	return c.Base.Create(ctx, rc)
}

func (c *RAGConfig) Update(ctx context.Context, rc *resource.Context) error {
	if err := c.resolveDatabaseHost(ctx, rc); err != nil {
		return err
	}
	var previous *common.RAGConfigResource
	var previousFailed bool
	if stored, ok := rc.State.Get(c.Identifier()); ok {
		prev, err := resource.ToResource[*RAGConfig](stored)
		if err != nil {
			return fmt.Errorf("failed to decode previous RAG config resource: %w", err)
		}
		previous = prev.Base
		previousFailed = stored.Error != ""
	}
	return c.Base.UpdateFrom(ctx, rc, previous, previousFailed)
}

func (c *RAGConfig) Delete(ctx context.Context, rc *resource.Context) error {
	return c.Base.Delete(ctx, rc)
}

func (c *RAGConfig) resolveDatabaseHost(ctx context.Context, rc *resource.Context) error {
	resolved, err := resolveServiceDatabaseHosts(ctx, rc, []string{c.DatabaseInstanceID})
	if err != nil {
		return err
	}
	c.Base.DatabaseHost = resolved[0].Host
	c.Base.DatabasePort = resolved[0].Port
	return nil
}
