package swarm

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/samber/do"
	"github.com/spf13/afero"

	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*LakekeeperConfigResource)(nil)

const ResourceTypeLakekeeperConfig resource.Type = "swarm.lakekeeper_config"

// lakekeeperReadyFile is the sentinel written on first Create so that
// subsequent reconciliations skip re-running Create unnecessarily.
const lakekeeperReadyFile = ".lakekeeper_ready"

func LakekeeperConfigResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeLakekeeperConfig,
	}
}

// LakekeeperConfigResource manages Lakekeeper's data directory on the host
// filesystem. It follows the same pattern as MCPConfigResource: it ensures
// any host-side artefacts needed by the container are present before the
// container starts.
//
// Lakekeeper is configured entirely via environment variables (LAKEKEEPER__*),
// so Create and Update are lightweight — they just write a sentinel file so
// Refresh can confirm the resource has been applied. Schema migration against
// the catalog Postgres is handled by the serve container itself, which runs it
// in-process on startup (LAKEKEEPER__DEBUG__MIGRATE_BEFORE_SERVE, set in
// ServiceContainerSpec) before it begins serving.
type LakekeeperConfigResource struct {
	ServiceInstanceID string `json:"service_instance_id"`
	ServiceID         string `json:"service_id"`
	HostID            string `json:"host_id"`
	DirResourceID     string `json:"dir_resource_id"`
}

func (r *LakekeeperConfigResource) ResourceVersion() string {
	return "1"
}

func (r *LakekeeperConfigResource) DiffIgnore() []string {
	return nil
}

func (r *LakekeeperConfigResource) Identifier() resource.Identifier {
	return LakekeeperConfigResourceIdentifier(r.ServiceInstanceID)
}

func (r *LakekeeperConfigResource) Executor() resource.Executor {
	return resource.HostExecutor(r.HostID)
}

func (r *LakekeeperConfigResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		filesystem.DirResourceIdentifier(r.DirResourceID),
	}
}

func (r *LakekeeperConfigResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *LakekeeperConfigResource) Refresh(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	dirPath, err := filesystem.DirResourceFullPath(rc, r.DirResourceID)
	if err != nil {
		return fmt.Errorf("failed to get service data dir path: %w", err)
	}

	// Check for the sentinel file; ErrNotFound here triggers Create.
	_, err = common.ReadResourceFile(fs, filepath.Join(dirPath, lakekeeperReadyFile))
	if err != nil {
		return fmt.Errorf("failed to read lakekeeper ready sentinel: %w", err)
	}

	return nil
}

func (r *LakekeeperConfigResource) Create(ctx context.Context, rc *resource.Context) error {
	fs, err := do.Invoke[afero.Fs](rc.Injector)
	if err != nil {
		return err
	}

	dirPath, err := filesystem.DirResourceFullPath(rc, r.DirResourceID)
	if err != nil {
		return fmt.Errorf("failed to get service data dir path: %w", err)
	}

	// Write the sentinel file so subsequent Refresh calls succeed.
	sentinelPath := filepath.Join(dirPath, lakekeeperReadyFile)
	if err := afero.WriteFile(fs, sentinelPath, []byte("ok\n"), 0o600); err != nil {
		return fmt.Errorf("failed to write lakekeeper ready sentinel: %w", err)
	}

	return nil
}

func (r *LakekeeperConfigResource) Update(ctx context.Context, rc *resource.Context) error {
	// No config files to regenerate; Lakekeeper configuration is delivered
	// via environment variables set in the container spec. Changes to
	// LAKEKEEPER__* env vars force a Swarm service restart automatically
	// because the ServiceInstanceSpec resource detects a diff in the desired
	// TaskTemplate.
	return nil
}

func (r *LakekeeperConfigResource) Delete(ctx context.Context, rc *resource.Context) error {
	// Cleanup is handled by the parent directory resource deletion.
	return nil
}
