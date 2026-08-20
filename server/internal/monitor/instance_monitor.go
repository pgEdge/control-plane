package monitor

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

// patroniRequestTimeout bounds the Patroni REST calls
// reconcileSynchronizedStandbySlots makes. patroni.NewClient's default
// http.Client has no request timeout of its own, and this reconciliation
// tends to have work to do right after a failover -- exactly when
// Patroni's REST API is most likely to be transiently unresponsive
// (mid-election) -- so an explicit bound here keeps a stalled connection
// from blocking this instance's status collection indefinitely.
const patroniRequestTimeout = 10 * time.Second

type InstanceMonitor struct {
	statusMonitor *Monitor
	databaseID    string
	instanceID    string
	dbName        string
	dbSvc         *database.Service
	certSvc       *certificates.Service
	logger        zerolog.Logger
}

func NewInstanceMonitor(
	dbSvc *database.Service,
	certSvc *certificates.Service,
	logger zerolog.Logger,
	databaseID string,
	instanceID string,
	dbName string,
) *InstanceMonitor {
	m := &InstanceMonitor{
		databaseID: databaseID,
		instanceID: instanceID,
		dbName:     dbName,
		dbSvc:      dbSvc,
		certSvc:    certSvc,
		logger:     logger,
	}
	m.statusMonitor = NewMonitor(
		logger,
		database.InstanceMonitorRefreshInterval,
		m.checkStatus,
	)
	return m
}

func (m *InstanceMonitor) Start(ctx context.Context) {
	m.statusMonitor.Start(ctx)
}

func (m *InstanceMonitor) Stop() {
	m.statusMonitor.Stop()
}

func (m *InstanceMonitor) checkStatus(ctx context.Context) error {
	status := &database.InstanceStatus{
		StatusUpdatedAt: utils.PointerTo(time.Now()),
	}
	dbState, err := m.dbSvc.GetStoredDatabaseState(ctx, m.databaseID)
	if err != nil {
		return m.updateInstanceErrStatus(ctx, status, fmt.Errorf("failed to get database state: %w", err))
	}

	info, err := m.dbSvc.GetInstanceConnectionInfo(ctx, m.databaseID, m.instanceID)
	if err != nil {
		if errors.Is(err, database.ErrInstanceStopped) {
			status.Stopped = utils.PointerTo(true)
			status.Error = utils.PointerTo(err.Error())
			return m.updateInstanceStatus(ctx, status)
		}
		return m.updateInstanceErrStatus(ctx, status, err)
	}
	status.Stopped = utils.PointerTo(false)
	status.Error = utils.PointerTo("")

	tlsCfg, err := m.certSvc.PostgresUserTLS(ctx, m.instanceID, info.InstanceHostname, "pgedge")
	if err != nil {
		return m.updateInstanceErrStatus(ctx, status, err)
	}

	status.Addresses = info.ClientAddresses
	status.Port = utils.PointerTo(info.ClientPort)

	err = m.populateFromPatroni(ctx, info, status)
	if err != nil {
		return m.updateInstanceErrStatus(ctx, status, err)
	}
	err = m.populateFromDbConn(ctx, dbState, info, tlsCfg, status)
	if err != nil {
		return m.updateInstanceErrStatus(ctx, status, err)
	}
	currentInstance, err := m.dbSvc.GetInstance(ctx, m.databaseID, m.instanceID)
	if err != nil {
		return m.updateInstanceErrStatus(ctx, status, err)
	}
	if currentInstance != nil && currentInstance.State != database.InstanceStateAvailable {
		_ = m.dbSvc.UpdateInstanceState(ctx, &database.InstanceStateUpdateOptions{
			InstanceID: m.instanceID,
			DatabaseID: m.databaseID,
			State:      database.InstanceStateAvailable,
		})
	}
	return m.updateInstanceStatus(ctx, status)
}

func (m *InstanceMonitor) populateFromPatroni(
	ctx context.Context,
	info *database.ConnectionInfo,
	status *database.InstanceStatus,
) error {
	client := patroni.NewClient(info.PatroniURL(), nil)
	patroniStatus, err := client.GetInstanceStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to get instance status: %w", err)
	}
	status.PatroniState = patroniStatus.State
	status.Role = patroniStatus.Role
	status.PatroniPaused = patroniStatus.Pause
	status.PendingRestart = patroniStatus.PendingRestart

	return nil
}

func (m *InstanceMonitor) populateFromDbConn(
	ctx context.Context,
	dbState database.DatabaseState,
	info *database.ConnectionInfo,
	tlsCfg *tls.Config,
	status *database.InstanceStatus,
) error {
	conn, err := database.ConnectToInstance(ctx, &database.ConnectionOptions{
		DSN: info.AdminDSN(m.dbName),
		TLS: tlsCfg,
	})
	if postgres.IsDatabaseNotExists(err) && dbState.IsInProgress() {
		// Skip database status collection if the database does not exist yet
		// and we're actively modifying the database.
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to connect to instance: %w", err)
	}
	defer conn.Close(ctx)

	pgVersion, err := postgres.GetPostgresVersion().Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to query postgres version: %w", err)
	}
	status.PostgresVersion = utils.PointerTo(pgVersion)

	spockVersion, err := postgres.GetSpockVersion().Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to query spock version: %w", err)
	}
	status.SpockVersion = utils.PointerTo(spockVersion)

	if status.IsPrimary() {
		spockReadOnly, err := postgres.GetSpockReadOnly().Scalar(ctx, conn)
		if err != nil {
			return fmt.Errorf("failed to query spock read-only status: %w", err)
		}
		status.ReadOnly = utils.PointerTo(spockReadOnly)

		subStatuses, err := postgres.GetSubscriptionStatuses().Scalars(ctx, conn)
		if err != nil {
			return fmt.Errorf("failed to query subscription statuses: %w", err)
		}
		for _, sub := range subStatuses {
			status.Subscriptions = append(status.Subscriptions, database.SubscriptionStatus{
				ProviderNode: sub.ProviderNode,
				Name:         sub.SubscriptionName,
				Status:       sub.Status,
			})
		}

		// Logged and swallowed rather than propagated: this is a
		// best-effort background reconciliation, not a health signal.
		// Letting it fail the whole status collection here would report
		// an otherwise-healthy primary as errored (discarding the
		// version/subscription data this same pass already collected)
		// over what's usually a transient Patroni REST hiccup -- and
		// since the reconciliation is self-correcting on every poll (see
		// its own doc comment), nothing is lost by retrying next tick
		// instead of surfacing this as an instance-level error now.
		if err := m.reconcileSynchronizedStandbySlots(ctx, conn, info, pgVersion, spockVersion); err != nil {
			m.logger.Err(err).
				Str("database_id", m.databaseID).
				Str("instance_id", m.instanceID).
				Msg("failed to reconcile synchronized_standby_slots")
		}
	}

	return nil
}

// reconcileSynchronizedStandbySlots keeps Postgres 17+'s
// synchronized_standby_slots GUC in sync with this node's actual current
// physical standby topology, on the current primary only. This is what
// makes native failover slots (see postgres.NeedsNativeFailoverSlots)
// safe to fail over onto: without it, a promoted replica's logical slots
// have no guarantee the outgoing primary's not-yet-decoded WAL was ever
// received by the physical standby that just became primary.
//
// This runs here, in the same 5s poll that already detects a role change
// (rather than e.g. a Patroni on_role_change callback), because it's the
// one thing in this codebase that already knows a role change happened
// -- Control Plane's own spec-driven reconciliation never runs on its
// own initiative when Patroni autonomously promotes a replica, and
// wiring a callback into the Postgres/Patroni container image would be
// new plumbing (script delivery, auth back to Control Plane) with no
// existing precedent anywhere in this codebase. See the design doc for
// the fuller comparison.
//
// Deliberately idempotent and self-correcting rather than cached: it
// re-derives the desired value and compares against the GUC's own live
// setting on every call, so a prior partial failure (e.g. the DCS patch
// below succeeds but the reload doesn't) is retried on the very next
// tick rather than silently stuck behind an in-memory "already handled"
// flag.
func (m *InstanceMonitor) reconcileSynchronizedStandbySlots(
	ctx context.Context,
	conn postgres.Executor,
	info *database.ConnectionInfo,
	pgVersionStr, spockVersionStr string,
) error {
	// A malformed version string here isn't treated as an error condition
	// -- it fails open to "not eligible," the same way
	// needsOutputPluginLibraries and nativeFailoverSlotMajors (see
	// postgres/gucs.go) treat an unresolvable version as "doesn't need
	// this" rather than an error. In practice this path is unreachable:
	// pgVersionStr/spockVersionStr come from GetPostgresVersion()/
	// GetSpockVersion(), which only ever produce clean, well-formed
	// version strings for a live connection.
	pgVersion, err := ds.ParseVersion(pgVersionStr)
	if err != nil {
		return nil
	}
	spockVersion, err := ds.ParseVersion(spockVersionStr)
	if err != nil {
		return nil
	}
	pgMajor, ok := pgVersion.Major()
	if !ok {
		return nil
	}
	spockMajor, ok := spockVersion.Major()
	if !ok {
		return nil
	}
	if !postgres.NeedsNativeFailoverSlots(spockMajor, pgMajor) {
		// Not a native-failover-slot cluster (e.g. Spock 5.x, or PG < 17)
		// -- leave synchronized_standby_slots alone entirely. Its
		// Postgres default is an empty string (no synchronization
		// requirement), so there's nothing to reconcile toward.
		return nil
	}

	slotNames, err := postgres.PhysicalReplicationSlotNames().Scalars(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to list physical replication slots: %w", err)
	}
	desired := strings.Join(slotNames, ",")

	current, err := postgres.CurrentSynchronizedStandbySlots().Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to read current synchronized_standby_slots: %w", err)
	}
	if current == desired {
		return nil
	}

	// Bounded independently of the caller's context: this reconciliation
	// is most likely to have work to do right after a failover, which is
	// exactly when Patroni's own REST API is most likely to be
	// transiently unresponsive (mid-election). http.DefaultClient (what
	// patroni.NewClient falls back to) has no request timeout of its
	// own, so without this a stalled connection here could block this
	// instance's status collection well past its usual 5s cadence.
	patchCtx, cancel := context.WithTimeout(ctx, patroniRequestTimeout)
	defer cancel()

	client := patroni.NewClient(info.PatroniURL(), nil)
	_, err = client.PatchDynamicConfig(patchCtx, &patroni.DynamicConfig{
		PostgreSQL: &patroni.DynamicPostgreSQLConfig{
			Parameters: utils.PointerTo(map[string]any{
				"synchronized_standby_slots": desired,
			}),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to patch synchronized_standby_slots to %q: %w", desired, err)
	}
	if err := client.Reload(patchCtx); err != nil {
		return fmt.Errorf("failed to reload after patching synchronized_standby_slots: %w", err)
	}

	return nil
}

func (m *InstanceMonitor) updateInstanceErrStatus(
	ctx context.Context,
	status *database.InstanceStatus,
	cause error,
) error {
	status.Error = utils.PointerTo(cause.Error())
	return m.updateInstanceStatus(ctx, status)
}

func (m *InstanceMonitor) updateInstanceStatus(ctx context.Context, status *database.InstanceStatus) error {
	err := m.dbSvc.UpdateInstanceStatus(ctx, m.databaseID, m.instanceID, status)
	if err != nil {
		return fmt.Errorf("failed to update instance status: %w", err)
	}

	return nil
}
