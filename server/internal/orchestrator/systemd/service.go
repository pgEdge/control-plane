package systemd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

const (
	// serviceHealthCheckTimeout bounds the HTTP request used to check whether
	// a service instance is serving.
	serviceHealthCheckTimeout = 5 * time.Second

	// Keys for the Control Plane metadata recorded in service unit files.
	serviceMetadataServiceType       = "ServiceType"
	serviceMetadataServiceInstanceID = "ServiceInstanceID"
	serviceMetadataPort              = "Port"
	serviceMetadataVersion           = "Version"
)

// supportedServiceTypes are the service types that can run on systemd hosts.
var supportedServiceTypes = []string{"postgrest", "mcp"}

func serviceUnitName(serviceType, serviceInstanceID string) string {
	return fmt.Sprintf("%s-%s.service", serviceType, serviceInstanceID)
}

// serviceUnit describes a service instance unit from the Control Plane
// metadata in its unit file.
type serviceUnit struct {
	Name        string
	ServiceType string
	Port        int
	Version     string
}

// findServiceUnit locates the unit file for the given service instance. It
// returns ErrUnitNotFound if no unit file exists.
func findServiceUnit(serviceInstanceID string) (*serviceUnit, error) {
	for _, serviceType := range supportedServiceTypes {
		name := serviceUnitName(serviceType, serviceInstanceID)
		options, err := readUnitFile(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}

		metadata := UnitMetadata(options)
		port, err := strconv.Atoi(metadata[serviceMetadataPort])
		if err != nil {
			return nil, fmt.Errorf("invalid port in metadata for unit '%s': %w", name, err)
		}

		return &serviceUnit{
			Name:        name,
			ServiceType: serviceType,
			Port:        port,
			Version:     metadata[serviceMetadataVersion],
		}, nil
	}

	return nil, ErrUnitNotFound
}

func (o *Orchestrator) ReloadServiceInstance(ctx context.Context, serviceInstanceID string, mode database.ReloadMode) error {
	unit, err := findServiceUnit(serviceInstanceID)
	if errors.Is(err, ErrUnitNotFound) {
		// The unit hasn't been written yet. It will read its current
		// configuration when it starts.
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to find unit for service instance '%s': %w", serviceInstanceID, err)
	}

	state, err := o.client.ActiveState(ctx, unit.Name)
	if err != nil {
		return err
	}
	if state != "active" && state != "reloading" {
		// The unit isn't running, so it will read its current configuration
		// when it next starts.
		return nil
	}

	switch mode {
	case database.ReloadModeRestart:
		return o.client.RestartUnit(ctx, unit.Name)
	case database.ReloadModeSighup:
		return o.client.SignalUnit(ctx, unit.Name, syscall.SIGHUP)
	case database.ReloadModeSigusr1:
		return o.client.SignalUnit(ctx, unit.Name, syscall.SIGUSR1)
	case database.ReloadModeSigusr2:
		return o.client.SignalUnit(ctx, unit.Name, syscall.SIGUSR2)
	default:
		return fmt.Errorf("unsupported reload mode: %s", mode)
	}
}

func (o *Orchestrator) GetServiceInstanceStatus(ctx context.Context, serviceInstanceID string) (*database.ServiceInstanceStatus, error) {
	unit, err := findServiceUnit(serviceInstanceID)
	if errors.Is(err, ErrUnitNotFound) {
		// Unit not written yet - return nil status (not an error) to match
		// the swarm orchestrator's behavior for a missing container.
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to find unit for service instance '%s': %w", serviceInstanceID, err)
	}

	state, err := o.client.ActiveState(ctx, unit.Name)
	if err != nil {
		return nil, err
	}
	if state != "active" && state != "reloading" {
		// Not running (starting, restarting, stopped or failed). A nil status
		// lets the monitor apply its grace periods before marking the
		// instance as failed.
		return nil, nil
	}

	status := &database.ServiceInstanceStatus{
		Addresses: o.cfg.ClientAddresses,
		Ports: []database.PortMapping{
			{
				Name:          "http",
				ContainerPort: unit.Port,
				HostPort:      utils.PointerTo(unit.Port),
			},
		},
		ServiceReady: utils.PointerTo(serviceServing(ctx, unit.ServiceType, unit.Port) == nil),
	}
	if unit.Version != "" {
		status.ImageVersion = utils.PointerTo(unit.Version)
	}

	return status, nil
}

// serviceServing checks that the service is accepting HTTP requests on the
// given port. For PostgREST, any response below 500 means that the service is
// up and able to serve requests, while server errors, e.g. its 503 when it
// can't reach the database, mean it isn't. The MCP server's health endpoint
// returns 200 once it's serving.
func serviceServing(ctx context.Context, serviceType string, port int) error {
	ctx, cancel := context.WithTimeout(ctx, serviceHealthCheckTimeout)
	defer cancel()

	path := "/"
	if serviceType == "mcp" {
		path = "/health"
	}
	url := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusInternalServerError ||
		(serviceType == "mcp" && resp.StatusCode != http.StatusOK) {
		return fmt.Errorf("service responded with status %d", resp.StatusCode)
	}

	return nil
}

func (o *Orchestrator) GenerateServiceInstanceResources(spec *database.ServiceInstanceSpec) (*database.ServiceInstanceResources, error) {
	switch spec.ServiceSpec.ServiceType {
	case "postgrest":
		return o.generatePostgRESTInstanceResources(spec)
	case "mcp":
		return o.generateMCPInstanceResources(spec)
	default:
		return nil, fmt.Errorf("service type %q is not supported on systemd hosts", spec.ServiceSpec.ServiceType)
	}
}

// lookupServiceUser returns the uid and gid of the Linux user that a service
// runs as, which its package creates.
func (o *Orchestrator) lookupServiceUser(username, pkg string) (int, int, error) {
	u, err := o.userLookup(username)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to look up the '%s' user, which should be created by the %s package: %w", username, pkg, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse uid for user '%s': %w", username, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse gid for user '%s': %w", username, err)
	}
	return uid, gid, nil
}

// installedServiceVersion returns the installed version of the given service,
// which must match the requested version unless it's "latest". Only one
// version of each service can be installed on a systemd host.
func (o *Orchestrator) installedServiceVersion(svc *database.ServiceSpec) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), serviceHealthCheckTimeout)
	defer cancel()

	installed, err := o.versionsProbe.InstalledServices(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get installed service versions: %w", err)
	}
	version, ok := installed[svc.ServiceType]
	if !ok {
		return "", fmt.Errorf("service type %q is not installed on this host", svc.ServiceType)
	}
	if svc.Version != host.ServiceVersionLatest && svc.Version != version {
		return "", fmt.Errorf("%s version %s is installed on this host, but version %s was requested", svc.ServiceType, version, svc.Version)
	}
	return version, nil
}

// serviceDatabaseInstances maps the spec's ordered database host list, which
// uses the swarm hostname convention, to database instance IDs. The instances'
// addresses and ports are resolved when the service's config is written,
// because ports may not be allocated yet when service resources are planned.
func serviceDatabaseInstances(spec *database.ServiceInstanceSpec) ([]string, error) {
	instanceIDs := map[string]string{}
	for _, node := range spec.DatabaseNodes {
		for _, inst := range node.Instances {
			instanceIDs[database.ServiceDatabaseHostname(inst.InstanceID)] = inst.InstanceID
		}
	}

	ids := make([]string, 0, len(spec.DatabaseHosts))
	for _, entry := range spec.DatabaseHosts {
		id, ok := instanceIDs[entry.Host]
		if !ok {
			return nil, fmt.Errorf("database host %q does not match any database instance", entry.Host)
		}
		ids = append(ids, id)
	}

	return ids, nil
}

// resolveServiceDatabaseHosts resolves each database instance to its host's
// peer address and its allocated port.
func resolveServiceDatabaseHosts(ctx context.Context, rc *resource.Context, instanceIDs []string) ([]database.ServiceHostEntry, error) {
	hostSvc, err := do.Invoke[*host.Service](rc.Injector)
	if err != nil {
		return nil, err
	}

	instances := make([]*database.InstanceResource, len(instanceIDs))
	hostIDs := ds.NewSet[string]()
	for i, id := range instanceIDs {
		inst, err := resource.FromContext[*database.InstanceResource](rc, database.InstanceResourceIdentifier(id))
		if err != nil {
			return nil, fmt.Errorf("failed to get database instance '%s': %w", id, err)
		}
		instances[i] = inst
		hostIDs.Add(inst.Spec.HostID)
	}

	found, err := hostSvc.GetHosts(ctx, hostIDs.ToSlice())
	if err != nil {
		return nil, fmt.Errorf("failed to get hosts: %w", err)
	}
	addresses := make(map[string]string, len(found))
	for _, h := range found {
		if len(h.PeerAddresses) == 0 {
			return nil, fmt.Errorf("host '%s' has no peer addresses", h.ID)
		}
		addresses[h.ID] = h.PeerAddresses[0]
	}

	entries := make([]database.ServiceHostEntry, len(instances))
	for i, inst := range instances {
		address, ok := addresses[inst.Spec.HostID]
		if !ok {
			return nil, fmt.Errorf("host '%s' not found", inst.Spec.HostID)
		}
		port := utils.FromPointer(inst.Spec.Port)
		if port == 0 {
			return nil, fmt.Errorf("port is not allocated for database instance '%s'", inst.Spec.InstanceID)
		}
		entries[i] = database.ServiceHostEntry{
			Host: address,
			Port: port,
		}
	}

	return entries, nil
}

// serviceNodeNames returns the service's own node followed by every other
// database node.
func serviceNodeNames(spec *database.ServiceInstanceSpec) []string {
	names := []string{spec.NodeName}
	for _, node := range spec.DatabaseNodes {
		if node.NodeName != spec.NodeName {
			names = append(names, node.NodeName)
		}
	}
	return names
}

func buildServiceInstanceResources(spec *database.ServiceInstanceSpec, resources []resource.Resource) (*database.ServiceInstanceResources, error) {
	data := make([]*resource.ResourceData, len(resources))
	for i, res := range resources {
		d, err := resource.ToResourceData(res)
		if err != nil {
			return nil, fmt.Errorf("failed to convert resource to resource data: %w", err)
		}
		data[i] = d
	}

	return &database.ServiceInstanceResources{
		ServiceInstance: &database.ServiceInstance{
			ServiceInstanceID: spec.ServiceInstanceID,
			ServiceID:         spec.ServiceSpec.ServiceID,
			DatabaseID:        spec.DatabaseID,
			HostID:            spec.HostID,
			State:             database.ServiceInstanceStateCreating,
		},
		Resources:         data,
		MonitorDependency: utils.PointerTo(ServiceInstanceResourceIdentifier(spec.ServiceInstanceID)),
	}, nil
}
