package database

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*InstanceResource)(nil)

const ResourceTypeInstance resource.Type = "database.instance"

func InstanceResourceIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypeInstance,
	}
}

type InstanceResource struct {
	Spec                       *InstanceSpec         `json:"spec"`
	InstanceHostname           string                `json:"instance_hostname"`
	PrimaryInstanceID          string                `json:"primary_instance_id"`
	PrimaryInstanceIDUpdatedAt time.Time             `json:"primary_instance_id_updated_at"`
	OrchestratorDependencies   []resource.Identifier `json:"dependencies"`
	ConnectionInfo             *ConnectionInfo       `json:"connection_info"`
	PostInit                   *Script               `json:"post_init"`
	// SpockSlotCopiesPending lists the subscriptions, as "provider→subscriber",
	// whose Spock slot this replica didn't yet hold a permanent copy of when
	// it was last created or updated. Only set for replicas that use native
	// slot sync. See waitForReplicationSlots.
	SpockSlotCopiesPending []string `json:"spock_slot_copies_pending,omitempty"`
}

func (r *InstanceResource) ResourceVersion() string {
	return "1"
}

func (r *InstanceResource) DiffIgnore() []string {
	return []string{
		"/primary_instance_id",
		"/primary_instance_id_updated_at",
		"/connection_info",
		"/spock_slot_copies_pending",
	}
}

func (r *InstanceResource) Executor() resource.Executor {
	return resource.HostExecutor(r.Spec.HostID)
}

func (r *InstanceResource) Identifier() resource.Identifier {
	return InstanceResourceIdentifier(r.Spec.InstanceID)
}

func (r *InstanceResource) Validate() error {
	var errs []error
	if r.Spec == nil {
		errs = append(errs, errors.New("spec: instance spec is required"))
	}
	return errors.Join(errs...)
}

func (r *InstanceResource) Dependencies() []resource.Identifier {
	dependencies := slices.Clone(r.OrchestratorDependencies)

	return dependencies
}

func (r *InstanceResource) TypeDependencies() []resource.Type {
	return nil
}

func (r *InstanceResource) Refresh(ctx context.Context, rc *resource.Context) error {
	if err := r.updateConnectionInfo(ctx, rc); err != nil {
		return resource.ErrNotFound
	}
	if err := r.updatePrimaryInstanceID(ctx, 30*time.Second); err != nil {
		return resource.ErrNotFound
	}
	if err := SetScriptNeedsToRun(ctx, rc, r.PostInit); err != nil {
		return err
	}

	return nil
}

func (r *InstanceResource) Create(ctx context.Context, rc *resource.Context) error {
	if err := r.updateConnectionInfo(ctx, rc); err != nil {
		return r.recordError(ctx, rc, err)
	}
	if err := WaitForPatroniRunning(ctx, r.patroniClient(), 0); err != nil {
		err = fmt.Errorf("failed to wait for patroni to enter running state: %w", err)
		return r.recordError(ctx, rc, err)
	}
	if err := r.initializeInstance(ctx, rc); err != nil {
		return r.recordError(ctx, rc, err)
	}

	return nil
}

func (r *InstanceResource) Update(ctx context.Context, rc *resource.Context) error {
	if err := r.updateConnectionInfo(ctx, rc); err != nil {
		return r.recordError(ctx, rc, err)
	}
	patroniClient := r.patroniClient()
	if err := WaitForPatroniRunning(ctx, patroniClient, 0); err != nil {
		err = fmt.Errorf("failed to wait for patroni to enter running state: %w", err)
		return r.recordError(ctx, rc, err)
	}
	if err := r.restartIfNeeded(ctx, patroniClient); err != nil {
		return r.recordError(ctx, rc, err)
	}
	if err := r.initializeInstance(ctx, rc); err != nil {
		return r.recordError(ctx, rc, err)
	}

	return nil
}

func (r *InstanceResource) Delete(ctx context.Context, rc *resource.Context) error {
	// It's unnecessary and potentially error-prone to delete anything here.
	// Instead, we just shut down the instance and delete the data from the
	// filesystem when we're fulfilling a delete request.
	return nil
}

func (r *InstanceResource) Connection(ctx context.Context, rc *resource.Context, dbName string) (*pgx.Conn, error) {
	if rc.HostID != r.Spec.HostID {
		return nil, fmt.Errorf("cannot connect to an instance running on a different host. executing host = '%s', instance host = '%s'", rc.HostID, r.Spec.HostID)
	}

	certs, err := do.Invoke[*certificates.Service](rc.Injector)
	if err != nil {
		return nil, err
	}

	tlsCfg, err := certs.PostgresUserTLS(ctx, r.Spec.InstanceID, r.InstanceHostname, "pgedge")
	if err != nil {
		return nil, fmt.Errorf("failed to get TLS config: %w", err)
	}

	conn, err := ConnectToInstance(ctx, &ConnectionOptions{
		DSN: r.ConnectionInfo.AdminDSN(dbName),
		TLS: tlsCfg,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database '%s': %w", dbName, err)
	}
	if err := postgres.SetSafeIdentifiers().Exec(ctx, conn); err != nil {
		return nil, fmt.Errorf("failed to set safe identifier settings: %w", err)
	}

	return conn, nil
}

func (r *InstanceResource) InstanceID() string {
	return r.Spec.InstanceID
}

func (r *InstanceResource) PostgresVersion() (*ds.Version, error) {
	if r.Spec.PgEdgeVersion == nil {
		return nil, errors.New("instance spec is missing a pgedge version")
	}
	if r.Spec.PgEdgeVersion.PostgresVersion == nil {
		return nil, errors.New("instance spec is missing a postgres version")
	}
	return r.Spec.PgEdgeVersion.PostgresVersion, nil
}

func (r *InstanceResource) Paths(orchestrator Orchestrator) (InstancePaths, error) {
	postgresVersion, err := r.PostgresVersion()
	if err != nil {
		return InstancePaths{}, err
	}
	paths, err := orchestrator.InstancePaths(postgresVersion, r.InstanceID())
	if err != nil {
		return InstancePaths{}, fmt.Errorf("failed to compute instance paths: %w", err)
	}
	return paths, nil
}

func (r *InstanceResource) updatePrimaryInstanceID(ctx context.Context, timeout time.Duration) error {
	primaryInstanceID, err := GetPrimaryInstanceID(ctx, r.patroniClient(), timeout)
	if err != nil {
		return err
	}
	r.PrimaryInstanceID = primaryInstanceID
	r.PrimaryInstanceIDUpdatedAt = time.Now()

	return nil
}

func (r *InstanceResource) initializeInstance(ctx context.Context, rc *resource.Context) error {
	// Set again below for replicas.
	r.SpockSlotCopiesPending = nil
	if err := r.updatePrimaryInstanceID(ctx, time.Minute); err != nil {
		return err
	}

	if r.Spec.InstanceID != r.PrimaryInstanceID {
		// Wait for this replica to hold copies of its primary's Spock slots
		// before marking it available, so that a later switchover to it
		// doesn't lose the slots that subscribers connected to this node use.
		if err := r.waitForReplicationSlots(ctx, rc); err != nil {
			return r.recordError(ctx, rc, err)
		}
		err := r.updateInstanceRecord(ctx, rc, &InstanceUpdateOptions{State: InstanceStateAvailable})
		if err != nil {
			return r.recordError(ctx, rc, err)
		}
		// no other initialization needed on non-primary instances
		return nil
	}

	conn, err := r.Connection(ctx, rc, "postgres")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	if err := ExecuteScript(ctx, rc, conn, r.PostInit); err != nil {
		return fmt.Errorf("failed to execute post-init script: %w", err)
	}

	// Spock shouldn't exist in the 'postgres' database, but we want to err on
	// the side of caution.
	tx, err := postgres.StartRepairModeTxn(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to start repair mode transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := r.createRoles(ctx, rc, tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	err = r.updateInstanceRecord(ctx, rc, &InstanceUpdateOptions{State: InstanceStateAvailable})
	if err != nil {
		return r.recordError(ctx, rc, err)
	}

	return nil
}

// waitForReplicationSlots polls pg_replication_slots on this replica until it
// has a permanent copy of the replication slot for every subscription where
// this node is the provider.
//
// Where Spock's own failover-slots worker copies the slots (Spock 5, and Spock
// 6 on Postgres 16), it syncs every spock.failover_slots_naptime: up to 60 s
// on Spock 5 and 1 s on Spock 6. The replica fails if the copies don't appear
// in time.
//
// Where native slot sync copies the slots (Spock 6 on Postgres 17+), the wait
// is best-effort. Native slot sync keeps a new copy temporary until the
// subscriber confirms a position past the one the standby reserved for it,
// which may not happen while the subscriber is down or paused. The replica
// doesn't fail then: it records the missing copies in SpockSlotCopiesPending,
// so that a rolling update doesn't switch over to it. Only copies that native
// slot sync made count. A copy left by Spock's own worker, or this instance's
// own slot from when it was a primary, has the same name as the primary's
// slot, and native slot sync won't take over a slot with the same name.
// Patroni doesn't drop them either, because Control Plane configures it to
// ignore Spock slots, so they are dropped here.
func (r *InstanceResource) waitForReplicationSlots(ctx context.Context, rc *resource.Context) error {
	r.SpockSlotCopiesPending = nil

	subs, err := resource.AllFromContext[*SubscriptionResource](rc, ResourceTypeSubscription)
	if err != nil {
		return fmt.Errorf("failed to list subscriptions: %w", err)
	}

	var relevant []*SubscriptionResource
	for _, sub := range subs {
		if sub.ProviderNode == r.Spec.NodeName {
			relevant = append(relevant, sub)
		}
	}
	if len(relevant) == 0 {
		return nil
	}

	conn, err := r.Connection(ctx, rc, r.Spec.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to connect for replication slot check: %w", err)
	}
	defer conn.Close(ctx)

	nativeSlotSync, err := r.usesNativeSlotSync(ctx, conn)
	if err != nil {
		return err
	}

	const (
		pollInterval    = 2 * time.Second
		slotWaitTimeout = 2 * time.Minute
	)
	deadline := time.Now().Add(slotWaitTimeout)
	for {
		if nativeSlotSync {
			if err := dropStaleSpockSlotCopies(ctx, conn); err != nil {
				return err
			}
		}
		var pending []string
		for _, sub := range relevant {
			ready := postgres.IsReplicationSlotReady(sub.DatabaseName, sub.ProviderNode, sub.SubscriberNode)
			if nativeSlotSync {
				ready = postgres.IsReplicationSlotSynced(sub.DatabaseName, sub.ProviderNode, sub.SubscriberNode)
			}
			exists, err := ready.Scalar(ctx, conn)
			if err != nil {
				return fmt.Errorf("failed to check replication slot for %s→%s: %w",
					sub.ProviderNode, sub.SubscriberNode, err)
			}
			if !exists {
				pending = append(pending, sub.ProviderNode+"→"+sub.SubscriberNode)
			}
		}
		if len(pending) == 0 {
			return nil
		}
		if !time.Now().Before(deadline) {
			if !nativeSlotSync {
				return fmt.Errorf("timed out waiting for a copy of the replication slot for %s", strings.Join(pending, ", "))
			}
			slices.Sort(pending)
			r.SpockSlotCopiesPending = pending
			if logger, err := do.Invoke[zerolog.Logger](rc.Injector); err == nil {
				logger.Warn().
					Str("instance_id", r.Spec.InstanceID).
					Strs("subscriptions", pending).
					Msg("replica doesn't hold permanent copies of all spock replication slots yet; it won't be switched over to until it does")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// usesNativeSlotSync reports whether this replica gets its Spock slot copies
// from PostgreSQL's native slot sync. That's configured for Spock 6 on
// Postgres 17+, and only applies while the instance is a standby.
func (r *InstanceResource) usesNativeSlotSync(ctx context.Context, conn *pgx.Conn) (bool, error) {
	if !postgres.NeedsNativeFailoverSlotsForVersion(r.Spec.PgEdgeVersion) {
		return false, nil
	}
	supported, err := SupportsSlotFailover(ctx, conn)
	if err != nil || !supported {
		return false, err
	}
	inRecovery, err := postgres.IsInRecovery().Scalar(ctx, conn)
	if err != nil {
		return false, fmt.Errorf("failed to check whether the instance is a standby: %w", err)
	}
	return inRecovery, nil
}

func (r *InstanceResource) updateInstanceRecord(ctx context.Context, rc *resource.Context, opts *InstanceUpdateOptions) error {
	svc, err := do.Invoke[*Service](rc.Injector)
	if err != nil {
		return err
	}
	opts.InstanceID = r.Spec.InstanceID
	opts.DatabaseID = r.Spec.DatabaseID
	opts.HostID = r.Spec.HostID
	opts.NodeName = r.Spec.NodeName
	opts.Port = r.Spec.Port
	opts.PatroniPort = r.Spec.PatroniPort
	opts.PgEdgeVersion = r.Spec.PgEdgeVersion
	err = svc.UpdateInstance(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to update instance state: %w", err)
	}

	return nil
}

func (r *InstanceResource) recordError(ctx context.Context, rc *resource.Context, cause error) error {
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}

	err = r.updateInstanceRecord(ctx, rc, &InstanceUpdateOptions{
		State: InstanceStateFailed,
		Error: cause.Error(),
	})
	if err != nil {
		logger.Err(err).Msg("failed to persist instance error status")
	}

	return cause
}

func (r *InstanceResource) updateConnectionInfo(ctx context.Context, rc *resource.Context) error {
	orch, err := do.Invoke[Orchestrator](rc.Injector)
	if err != nil {
		return err
	}
	connInfo, err := orch.GetInstanceConnectionInfo(ctx,
		r.Spec.DatabaseID, r.Spec.InstanceID,
		r.Spec.Port, r.Spec.PatroniPort,
		r.Spec.PgEdgeVersion,
	)
	if err != nil {
		return fmt.Errorf("failed to get instance connection info: %w", err)
	}
	r.ConnectionInfo = connInfo

	return nil
}

func (r *InstanceResource) patroniClient() *patroni.Client {
	return patroni.NewClient(r.ConnectionInfo.PatroniURL(), nil)
}

func (r *InstanceResource) createRoles(ctx context.Context, rc *resource.Context, tx pgx.Tx) error {
	source, err := resource.FromContext[*RolesSourceResource](rc, RolesSourceResourceIdentifier(r.Spec.NodeName))
	if errors.Is(err, resource.ErrNotFound) {
		return r.createRolesFromSpec(ctx, tx)
	} else if err != nil {
		return fmt.Errorf("failed to check for roles source: %w", err)
	}

	dump, err := resource.FromContext[*DumpRolesResource](rc, DumpRolesResourceIdentifier(source.SourceNodeName))
	if err != nil {
		return fmt.Errorf("failed to find roles dump from source node '%s': %w", source.SourceNodeName, err)
	}

	return r.createRolesFromDump(ctx, dump, tx)
}

func (r *InstanceResource) createRolesFromDump(ctx context.Context, dump *DumpRolesResource, tx pgx.Tx) error {
	for i, role := range dump.Roles {
		err := postgres.CreateRoleIfNotExists(role).Exec(ctx, tx)
		if err != nil {
			return fmt.Errorf("failed to create role[%d] from roles dump: %w", i, err)
		}
	}
	for i, statement := range dump.Statements {
		_, err := tx.Exec(ctx, statement)
		if err != nil {
			return fmt.Errorf("failed to execute statement[%d] from roles dump: %w", i, err)
		}
	}

	return nil
}

func (r *InstanceResource) createRolesFromSpec(ctx context.Context, tx pgx.Tx) error {
	roleStatements, err := postgres.CreateBuiltInRoles(postgres.BuiltinRoleOptions{
		PGVersion: r.Spec.PgEdgeVersion.PostgresVersion.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to generate built-in role statements: %w", err)
	}
	if err := roleStatements.Exec(ctx, tx); err != nil {
		return fmt.Errorf("failed to create built-in roles: %w", err)
	}

	for _, user := range r.Spec.DatabaseUsers {
		statement, err := postgres.CreateUserRole(postgres.UserRoleOptions{
			Name:       user.Username,
			Password:   user.Password,
			Attributes: user.Attributes,
			Roles:      user.Roles,
		})
		if err != nil {
			return fmt.Errorf("failed to produce create user role statement %q: %w", user.Username, err)
		}
		if err := statement.Exec(ctx, tx); err != nil {
			return fmt.Errorf("failed to create user role %q: %w", user.Username, err)
		}
	}

	return nil
}

func (r *InstanceResource) restartIfNeeded(ctx context.Context, client *patroni.Client) error {
	status, err := client.GetInstanceStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to get patroni instance status: %w", err)
	}
	if status.PendingRestart == nil || !*status.PendingRestart {
		return nil
	}
	if err := client.ScheduleRestart(ctx, &patroni.Restart{}); err != nil {
		return fmt.Errorf("failed to submit restart request: %w", err)
	}
	if err := WaitForPatroniRunning(ctx, client, 0); err != nil {
		return fmt.Errorf("failed to wait for patroni to re-enter a running state after restart: %w", err)
	}

	return nil
}
