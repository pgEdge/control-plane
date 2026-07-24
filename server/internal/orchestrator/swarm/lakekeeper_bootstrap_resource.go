package swarm

import (
	"context"
	"fmt"
	"net/http"

	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*LakekeeperBootstrapResource)(nil)

const ResourceTypeLakekeeperBootstrap resource.Type = "swarm.lakekeeper_bootstrap"

func LakekeeperBootstrapResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeLakekeeperBootstrap,
	}
}

// LakekeeperBootstrapResource performs the post-deploy REST bootstrap of a
// Lakekeeper serve instance: it accepts the terms of use, creates the
// configured warehouse (with its object-store storage profile and credential),
// and creates the "default" namespace so that compaction can create tables.
//
// It depends on the serve ServiceInstanceResource, so it only runs after the
// Docker Swarm service is confirmed healthy (WaitForService). It runs on the
// same host as the serve container (HostExecutor) and reaches Lakekeeper at the
// serve container's IP on the Docker default bridge network — the same scheme
// GetInstanceConnectionInfo uses to reach Patroni/Postgres. It does NOT use the
// swarm service name: that resolves only via overlay DNS from inside the overlay,
// which the host-networked CP process is not a member of (finding #14).
//
// A bootstrap FAILURE blocks: a database with an unbootstrapped warehouse is
// broken, so the error is returned from Create (surfaced by the resource
// engine) rather than merely logged in a best-effort PostDeploy hook.
//
// The sequence is idempotent — already-bootstrapped / already-exists responses
// are treated as success — so Create and Update re-run safely. BootstrapDone is
// a sentinel so Refresh can distinguish "never run" from "already applied".
type LakekeeperBootstrapResource struct {
	ServiceInstanceID string         `json:"service_instance_id"`
	HostID            string         `json:"host_id"`
	ServiceName       string         `json:"service_name"`
	Port              int            `json:"port"`
	Config            map[string]any `json:"config"`
	BootstrapDone     bool           `json:"bootstrap_done"`
}

func (r *LakekeeperBootstrapResource) ResourceVersion() string { return "1" }

func (r *LakekeeperBootstrapResource) DiffIgnore() []string {
	// BootstrapDone is runtime state written by Create; exclude it from diffs
	// so a completed bootstrap does not trigger spurious updates.
	return []string{"/bootstrap_done"}
}

func (r *LakekeeperBootstrapResource) Identifier() resource.Identifier {
	return LakekeeperBootstrapResourceIdentifier(r.ServiceInstanceID)
}

func (r *LakekeeperBootstrapResource) Executor() resource.Executor {
	// Run on the same host as the serve container so we can reach Lakekeeper
	// over the bridge network and share its Docker network context.
	return resource.HostExecutor(r.HostID)
}

func (r *LakekeeperBootstrapResource) Dependencies() []resource.Identifier {
	// Depend on the serve ServiceInstanceResource so bootstrap only runs after
	// the Docker service is confirmed healthy.
	return []resource.Identifier{
		ServiceInstanceResourceIdentifier(r.ServiceInstanceID),
	}
}

func (r *LakekeeperBootstrapResource) TypeDependencies() []resource.Type {
	return nil
}

// Refresh returns ErrNotFound until the bootstrap has completed at least once,
// causing the resource engine to call Create.
func (r *LakekeeperBootstrapResource) Refresh(ctx context.Context, rc *resource.Context) error {
	if !r.BootstrapDone {
		return fmt.Errorf("%w: lakekeeper warehouse bootstrap has not yet run", resource.ErrNotFound)
	}
	return nil
}

func (r *LakekeeperBootstrapResource) Create(ctx context.Context, rc *resource.Context) error {
	if err := r.bootstrap(ctx, rc); err != nil {
		return err
	}
	r.BootstrapDone = true
	return nil
}

func (r *LakekeeperBootstrapResource) Update(ctx context.Context, rc *resource.Context) error {
	// Re-running the bootstrap is safe: the REST sequence is idempotent.
	if err := r.bootstrap(ctx, rc); err != nil {
		return err
	}
	r.BootstrapDone = true
	return nil
}

func (r *LakekeeperBootstrapResource) Delete(ctx context.Context, rc *resource.Context) error {
	// Deleting the service removes the warehouse configuration; nothing to do.
	return nil
}

func (r *LakekeeperBootstrapResource) bootstrap(ctx context.Context, rc *resource.Context) error {
	cfg, err := parseLakekeeperStorageConfig(r.Config)
	if err != nil {
		return err
	}

	host, err := r.serveBridgeHost(ctx, rc)
	if err != nil {
		return err
	}
	baseURL := lakekeeperBaseURL(host, r.Port)

	client := &http.Client{Timeout: lakekeeperBootstrapHTTPTimeout}
	if err := runLakekeeperBootstrap(ctx, client, baseURL, cfg); err != nil {
		return err
	}
	return nil
}

// serveBridgeHost returns the serve container's IP on the Docker default bridge
// network, which the host-networked CP process can reach directly — mirroring how
// GetInstanceConnectionInfo reaches Patroni/Postgres. The serve container is
// found by its service instance ID and inspected fresh each run (a rescheduled
// task gets a new bridge IP).
func (r *LakekeeperBootstrapResource) serveBridgeHost(ctx context.Context, rc *resource.Context) (string, error) {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return "", fmt.Errorf("lakekeeper bootstrap: failed to get docker client: %w", err)
	}
	c, err := GetServiceContainer(ctx, client, r.ServiceInstanceID)
	if err != nil {
		return "", fmt.Errorf("lakekeeper bootstrap: failed to find serve container: %w", err)
	}
	inspect, err := client.ContainerInspect(ctx, c.ID)
	if err != nil {
		return "", fmt.Errorf("lakekeeper bootstrap: failed to inspect serve container: %w", err)
	}
	host, err := bridgeIPAddress(inspect)
	if err != nil {
		return "", fmt.Errorf("lakekeeper bootstrap: serve container %q: %w", c.ID, err)
	}
	return host, nil
}

// lakekeeperBaseURL builds the Lakekeeper REST base URL. A zero port falls back
// to the in-container listen port 8181 (reachable on the bridge IP regardless of
// any host-port publishing).
func lakekeeperBaseURL(host string, port int) string {
	if port == 0 {
		port = 8181
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}
