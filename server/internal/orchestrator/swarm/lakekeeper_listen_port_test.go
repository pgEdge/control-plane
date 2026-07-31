package swarm

import (
	"fmt"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/scheduler"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

// A lakekeeper service instance is created with Port 0, meaning "allocate a
// random host port", and the control plane then replaces it with a real
// allocated HOST port that becomes the swarm service's PublishedPort. The
// container itself always listens on lakekeeperListenPort.
//
// Both ColdFront call sites address the container directly — the bootstrap via
// the serve container's bridge IP, and the coldfront.lakekeeper_endpoint GUC and
// tiering endpoint via the swarm service name on the database overlay. None of
// those addresses exposes the published host port, so all must use the
// in-container listen port. Pairing a container address with the published port
// yields a connection-refused that nothing recovers from.
const testAllocatedHostPort = 6509

func TestLakekeeperBootstrapUsesListenPortNotPublishedPort(t *testing.T) {
	o := newLakekeeperTestOrchestrator(t)
	spec := makeLakekeeperSpecWithStorage()
	spec.Port = utils.PointerTo(testAllocatedHostPort)

	result, err := o.generateLakekeeperInstanceResources(spec)
	if err != nil {
		t.Fatalf("generateLakekeeperInstanceResources() unexpected error: %v", err)
	}

	var found bool
	for _, rd := range result.Resources {
		if rd.Identifier.Type != ResourceTypeLakekeeperBootstrap {
			continue
		}
		br, decErr := resource.ToResource[*LakekeeperBootstrapResource](rd)
		if decErr != nil {
			t.Fatalf("failed to decode LakekeeperBootstrapResource: %v", decErr)
		}
		found = true
		if br.Port != lakekeeperListenPort {
			t.Errorf("bootstrap Port = %d, want %d (the in-container listen port); "+
				"the published host port is not reachable on the container bridge IP",
				br.Port, lakekeeperListenPort)
		}
	}
	if !found {
		t.Fatal("no LakekeeperBootstrapResource in the generated resources")
	}
}

func TestLakekeeperGUCEndpointUsesListenPortNotPublishedPort(t *testing.T) {
	o := newLakekeeperTestOrchestrator(t)
	spec := makeLakekeeperSpecWithStorage()
	spec.Port = utils.PointerTo(testAllocatedHostPort)

	result, err := o.generateLakekeeperInstanceResources(spec)
	if err != nil {
		t.Fatalf("generateLakekeeperInstanceResources() unexpected error: %v", err)
	}

	wantEndpoint := fmt.Sprintf("http://%s:%d/catalog",
		ServiceInstanceName(spec.DatabaseID, spec.ServiceSpec.ServiceID, spec.HostID),
		lakekeeperListenPort)

	var found bool
	for _, rd := range result.Resources {
		if rd.Identifier.Type != ResourceTypeLakekeeperStorageSecret {
			continue
		}
		sr, decErr := resource.ToResource[*LakekeeperStorageSecretResource](rd)
		if decErr != nil {
			t.Fatalf("failed to decode LakekeeperStorageSecretResource: %v", decErr)
		}
		found = true
		if sr.LakekeeperEndpoint != wantEndpoint {
			t.Errorf("lakekeeper_endpoint GUC = %q, want %q; the swarm service name "+
				"resolves on the database overlay, where the published host port "+
				"does not exist", sr.LakekeeperEndpoint, wantEndpoint)
		}
	}
	if !found {
		t.Fatal("no LakekeeperStorageSecretResource in the generated resources")
	}
}

// The tiering binaries run via docker exec inside the Postgres container and
// reach the catalog over the same database overlay, so their endpoint is
// subject to the identical published-vs-listen port trap.
func TestLakekeeperTieringEndpointUsesListenPortNotPublishedPort(t *testing.T) {
	o := newLakekeeperTestOrchestrator(t)
	spec := makeLakekeeperSpecWithStorage()
	spec.Port = utils.PointerTo(testAllocatedHostPort)

	result, err := o.generateLakekeeperInstanceResources(spec)
	if err != nil {
		t.Fatalf("generateLakekeeperInstanceResources() unexpected error: %v", err)
	}

	// The catalog root, matching ColdFront's documented lakekeeper_endpoint
	// contract: the compactor hands this value straight to iceberg-go's REST
	// catalog, and Lakekeeper serves the Iceberg REST API under /catalog.
	wantEndpoint := fmt.Sprintf("http://%s:%d/catalog",
		ServiceInstanceName(spec.DatabaseID, spec.ServiceSpec.ServiceID, spec.HostID),
		lakekeeperListenPort)

	var checked int
	for _, rd := range result.Resources {
		if rd.Identifier.Type != scheduler.ResourceTypeScheduledJob {
			continue
		}
		job, decErr := resource.ToResource[*scheduler.ScheduledJobResource](rd)
		if decErr != nil {
			t.Fatalf("failed to decode ScheduledJobResource %q: %v", rd.Identifier.ID, decErr)
		}
		svcCfg, ok := job.Args["service_config"].(map[string]any)
		if !ok {
			t.Fatalf("job %q: service_config missing or wrong type: %T",
				rd.Identifier.ID, job.Args["service_config"])
		}
		if got := svcCfg["lakekeeper_endpoint"]; got != wantEndpoint {
			t.Errorf("job %q: lakekeeper_endpoint = %v, want %q",
				rd.Identifier.ID, got, wantEndpoint)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no tiering ScheduledJobResource found to assert on")
	}
}

// Guards against an over-eager "replace every spec.Port with the constant"
// refactor: the swarm service must still PUBLISH the allocated host port while
// TARGETING the in-container listen port. Distinguishable only when the two
// differ, which TestServiceContainerSpec_Lakekeeper_Port8181 cannot show.
func TestServiceContainerSpec_LakekeeperPublishesHostPortTargetsListenPort(t *testing.T) {
	opts := makeLakekeeperSpecOpts()
	opts.Port = utils.PointerTo(testAllocatedHostPort)

	spec, err := ServiceContainerSpec(opts)
	if err != nil {
		t.Fatalf("ServiceContainerSpec() unexpected error: %v", err)
	}

	ports := spec.EndpointSpec.Ports
	if len(ports) != 1 {
		t.Fatalf("got %d published ports, want 1", len(ports))
	}
	if got := ports[0].TargetPort; got != uint32(lakekeeperListenPort) {
		t.Errorf("TargetPort = %d, want %d (in-container listen port)",
			got, lakekeeperListenPort)
	}
	if got := ports[0].PublishedPort; got != uint32(testAllocatedHostPort) {
		t.Errorf("PublishedPort = %d, want %d (allocated host port)",
			got, testAllocatedHostPort)
	}
}
