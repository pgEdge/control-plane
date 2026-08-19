package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var minSpockVersionForSyncEventArgs = ds.MustParseVersion(postgres.MinSpockVersionForSyncEventArgs)

// spockSupportsSyncEventArgs reports whether conn's Spock version is new
// enough for spock.sync_event(boolean) and the 5-arg
// spock.wait_for_sync_event(..., wait_if_disabled) — see
// postgres.MinSpockVersionForSyncEventArgs.
func spockSupportsSyncEventArgs(ctx context.Context, conn *pgx.Conn) (bool, error) {
	versionStr, err := postgres.GetSpockVersion().Scalar(ctx, conn)
	if err != nil {
		return false, fmt.Errorf("failed to get spock version: %w", err)
	}
	version, err := ds.ParseVersion(versionStr)
	if err != nil {
		return false, fmt.Errorf("failed to parse spock version %q: %w", versionStr, err)
	}
	return version.Compare(minSpockVersionForSyncEventArgs) >= 0, nil
}

var _ resource.Resource = (*SyncEventResource)(nil)

const ResourceTypeSyncEvent resource.Type = "database.sync_event"

func SyncEventResourceIdentifier(providerNode, subscriberNode, databaseName string) resource.Identifier {
	return resource.Identifier{
		Type: ResourceTypeSyncEvent,
		ID:   fmt.Sprintf("%s:%s:%s", providerNode, subscriberNode, databaseName),
	}
}

type SyncEventResource struct {
	DatabaseName      string                `json:"database_name"`
	ProviderNode      string                `json:"provider_node"`
	SubscriberNode    string                `json:"subscriber_node"`
	SyncEventLsn      string                `json:"sync_event_lsn"`
	ExtraDependencies []resource.Identifier `json:"extra_dependencies"`
}

func (r *SyncEventResource) ResourceVersion() string {
	return "1"
}

func (r *SyncEventResource) DiffIgnore() []string {
	return nil
}

func (r *SyncEventResource) Executor() resource.Executor {
	return resource.PrimaryExecutor(r.ProviderNode)
}

func (r *SyncEventResource) Identifier() resource.Identifier {
	return SyncEventResourceIdentifier(r.ProviderNode, r.SubscriberNode, r.DatabaseName)
}

func (r *SyncEventResource) Dependencies() []resource.Identifier {
	deps := []resource.Identifier{
		SubscriptionResourceIdentifier(r.ProviderNode, r.SubscriberNode, r.DatabaseName),
	}

	deps = append(deps, r.ExtraDependencies...)
	return deps
}

func (r *SyncEventResource) TypeDependencies() []resource.Type {
	return nil
}

// Confirm synchronization by sending sync_event from provider and waiting for it on subscriber
func (r *SyncEventResource) Refresh(ctx context.Context, rc *resource.Context) error {
	// Get provider instance
	provider, err := GetPrimaryInstance(ctx, rc, r.ProviderNode)
	if err != nil {
		return fmt.Errorf("failed to get provider instance: %w", err)
	}
	providerConn, err := provider.Connection(ctx, rc, r.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect to provider database %q: %w", r.DatabaseName, err)
	}
	defer providerConn.Close(ctx)

	transactional, err := spockSupportsSyncEventArgs(ctx, providerConn)
	if err != nil {
		return fmt.Errorf("failed to check spock version on provider: %w", err)
	}

	// Send sync event from provider
	lsn, err := postgres.SyncEvent(transactional).Scalar(ctx, providerConn)
	if err != nil {
		if postgres.IsSpockNodeNotConfigured(err) {
			return resource.ErrNotFound
		}
		return fmt.Errorf("failed to send sync event from provider: %w", err)
	}

	r.SyncEventLsn = lsn

	return nil
}

func (r *SyncEventResource) Create(ctx context.Context, rc *resource.Context) error {
	// Confirm sync is a no-op for create, just call Refresh
	return r.Refresh(ctx, rc)
}

func (r *SyncEventResource) Update(ctx context.Context, rc *resource.Context) error {
	// No-op for update
	return nil
}

func (r *SyncEventResource) Delete(ctx context.Context, rc *resource.Context) error {
	// No-op for delete
	return nil
}
