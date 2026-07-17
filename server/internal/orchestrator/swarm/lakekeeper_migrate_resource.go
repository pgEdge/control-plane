package swarm

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*LakekeeperMigrateResource)(nil)

const ResourceTypeLakekeeperMigrate resource.Type = "swarm.lakekeeper_migrate"

// lakekeeperMigrateTimeout is the maximum time allowed for the schema migration
// to complete. Migrations run once on the external catalog Postgres and are
// expected to be fast, but a generous timeout is used to tolerate slow cold
// starts or transient network delays.
const lakekeeperMigrateTimeout = 5 * time.Minute

func LakekeeperMigrateResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeLakekeeperMigrate,
	}
}

// LakekeeperMigrateResource runs the Lakekeeper image with the "migrate"
// subcommand as a one-shot Docker container before the "serve" container is
// started. It applies the Iceberg catalog schema to the external Postgres
// database supplied via LAKEKEEPER__PG_DATABASE_URL_{READ,WRITE}.
//
// The resource is idempotent: Lakekeeper's own migrate command is a no-op when
// the schema is already current, so Create and Update both re-run the
// migration safely on every reconciliation cycle.
//
// Lifecycle:
//   - Refresh: returns ErrNotFound until migration has completed at least once
//     successfully (detected via the sentinel stored in MigratedOnce). This
//     causes the resource engine to call Create on the first pass and then
//     re-validate on subsequent passes.
//   - Create/Update: run the migrate container and wait for exit-0.
//   - Delete: no-op; deleting the service removes the catalog DB connection.
type LakekeeperMigrateResource struct {
	ServiceInstanceID string `json:"service_instance_id"`
	HostID            string `json:"host_id"`
	Image             string `json:"image"`
	CatalogDBURL      string `json:"catalog_db_url"`
	PGEncryptionKey   string `json:"pg_encryption_key"`
	// CatalogDBManaged marks the catalog database as control-plane
	// managed (spec key catalog_db_create): migration must wait for the
	// LakekeeperCatalogDBResource to create it.
	CatalogDBManaged bool `json:"catalog_db_managed"`
	// MigratedOnce is set to true after the first successful migration so that
	// Refresh can distinguish "never run" from "already applied".
	MigratedOnce bool `json:"migrated_once"`
}

func (r *LakekeeperMigrateResource) ResourceVersion() string { return "1" }

func (r *LakekeeperMigrateResource) DiffIgnore() []string {
	// MigratedOnce is runtime state written by Create; exclude it from diff
	// comparisons so that a completed migration does not trigger spurious updates.
	return []string{"/migrated_once"}
}

func (r *LakekeeperMigrateResource) Identifier() resource.Identifier {
	return LakekeeperMigrateResourceIdentifier(r.ServiceInstanceID)
}

func (r *LakekeeperMigrateResource) Executor() resource.Executor {
	// The migration container is run on the same host as the serve container
	// so that it shares the same Docker daemon and network context.
	return resource.HostExecutor(r.HostID)
}

func (r *LakekeeperMigrateResource) Dependencies() []resource.Identifier {
	// When control-plane manages the catalog database, migration must run
	// after it exists. For an external catalog there is no resource to
	// depend on; the URL is validated at spec time (fail-loud check).
	if r.CatalogDBManaged {
		return []resource.Identifier{
			LakekeeperCatalogDBResourceIdentifier(r.ServiceInstanceID),
		}
	}
	return nil
}

func (r *LakekeeperMigrateResource) TypeDependencies() []resource.Type {
	return nil
}

// Refresh returns ErrNotFound when migration has never successfully completed,
// causing the resource engine to call Create. Once MigratedOnce is true the
// resource is considered up-to-date and no further action is taken unless the
// desired state changes (which triggers Update).
func (r *LakekeeperMigrateResource) Refresh(ctx context.Context, rc *resource.Context) error {
	if !r.MigratedOnce {
		return fmt.Errorf("%w: lakekeeper schema migration has not yet run", resource.ErrNotFound)
	}
	return nil
}

func (r *LakekeeperMigrateResource) Create(ctx context.Context, rc *resource.Context) error {
	if err := r.runMigrate(ctx, rc); err != nil {
		return err
	}
	r.MigratedOnce = true
	return nil
}

func (r *LakekeeperMigrateResource) Update(ctx context.Context, rc *resource.Context) error {
	// Re-running migrate is always safe: Lakekeeper's migrate is idempotent.
	if err := r.runMigrate(ctx, rc); err != nil {
		return err
	}
	r.MigratedOnce = true
	return nil
}

func (r *LakekeeperMigrateResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}

// runMigrate starts a one-shot Docker container using the Lakekeeper image
// with the "migrate" subcommand, waits for it to exit with status 0, and
// streams the logs to the resource context logger on failure.
func (r *LakekeeperMigrateResource) runMigrate(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return fmt.Errorf("lakekeeper migrate: failed to get docker client: %w", err)
	}

	containerName := "lakekeeper-migrate-" + r.ServiceInstanceID

	// Remove any leftover container from a previous failed attempt before
	// creating a new one, so the name does not conflict.
	_ = client.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})

	containerID, err := client.ContainerRun(ctx, docker.ContainerRunOptions{
		Config: &container.Config{
			Image: r.Image,
			Cmd:   []string{"migrate"},
			Env: []string{
				"LAKEKEEPER__PG_DATABASE_URL_READ=" + r.CatalogDBURL,
				"LAKEKEEPER__PG_DATABASE_URL_WRITE=" + r.CatalogDBURL,
				"LAKEKEEPER__PG_ENCRYPTION_KEY=" + r.PGEncryptionKey,
			},
		},
		Host: &container.HostConfig{
			// AutoRemove is intentionally false so that we can stream logs on
			// failure before the container is cleaned up.
			AutoRemove: false,
		},
		Name: containerName,
	})
	if err != nil {
		return fmt.Errorf("lakekeeper migrate: failed to start container: %w", err)
	}

	// Wait for the migration to complete.
	waitErr := client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning, lakekeeperMigrateTimeout)

	// Always remove the container once we have the exit status (or on error).
	defer func() {
		removeCtx := context.Background()
		_ = client.ContainerRemove(removeCtx, containerID, container.RemoveOptions{Force: true})
	}()

	if waitErr != nil {
		// Capture logs to help diagnose the failure.
		var logBuf bytes.Buffer
		_ = client.ContainerLogs(ctx, &logBuf, containerID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
		})
		return fmt.Errorf("lakekeeper migrate: migration failed: %w\nlogs:\n%s", waitErr, logBuf.String())
	}

	return nil
}
