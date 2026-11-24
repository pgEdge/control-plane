package database

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

// pg17VersionNum is the first server_version_num with logical slot failover
// (the failover and synced columns, ALTER_REPLICATION_SLOT, and native slot
// sync).
const pg17VersionNum = 170000

// ConnectReplicationToInstance opens a logical replication connection
// (replication=database) to the instance, for replication protocol commands
// such as ALTER_REPLICATION_SLOT. Only the simple query protocol can be used
// on it.
func ConnectReplicationToInstance(ctx context.Context, opts *ConnectionOptions) (*pgconn.PgConn, error) {
	dsn := *opts.DSN
	dsn.Extra = maps.Clone(opts.DSN.Extra)
	if dsn.Extra == nil {
		dsn.Extra = map[string]string{}
	}
	dsn.Extra["replication"] = "database"

	conf, err := pgconn.ParseConfig(dsn.String())
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}
	conf.TLSConfig = opts.TLS
	conn, err := pgconn.ConnectConfig(ctx, conf)
	if err != nil {
		return nil, fmt.Errorf("failed to open replication connection to instance: %w", err)
	}
	return conn, nil
}

// SupportsSlotFailover reports whether the connected server has logical slot
// failover (PostgreSQL 17 or later).
func SupportsSlotFailover(ctx context.Context, conn *pgx.Conn) (bool, error) {
	versionNum, err := postgres.ServerVersionNum().Scalar(ctx, conn)
	if err != nil {
		return false, fmt.Errorf("failed to get server version: %w", err)
	}
	return versionNum >= pg17VersionNum, nil
}

// EnableSpockSlotsFailover sets the failover flag on every Spock slot on this
// primary that lacks it, so PostgreSQL's native slot sync copies the slot to
// standbys. conn and repl must both be connected to the same primary, repl as
// a replication connection. Requires PostgreSQL 17 or later.
//
// A slot can only be altered while no walsender holds it, which on a live
// cluster is most of the time. Spock's own spock.slot_enable_failover() skips
// such slots, and Spock 5 has no SQL to set the flag at all, so this ends the
// slot's walsender and alters the slot before the subscriber reconnects. The
// subscriber's apply worker reconnects after its restart delay and resumes
// from the slot's position, so no changes are lost.
func EnableSpockSlotsFailover(ctx context.Context, conn *pgx.Conn, repl *pgconn.PgConn) error {
	const maxAttempts = 10

	for attempt := 1; ; attempt++ {
		slots, err := postgres.SpockSlotsWithoutFailover().Structs(ctx, conn)
		if err != nil {
			return fmt.Errorf("failed to list spock replication slots without failover: %w", err)
		}
		if len(slots) == 0 {
			return nil
		}
		if attempt > maxAttempts {
			names := make([]string, len(slots))
			for i, slot := range slots {
				names[i] = slot.Name
			}
			return fmt.Errorf("failed to enable failover on spock replication slots %s: they stayed in use", strings.Join(names, ", "))
		}
		for _, slot := range slots {
			if err := enableSlotFailover(ctx, conn, repl, slot); err != nil {
				return err
			}
		}
	}
}

func enableSlotFailover(ctx context.Context, conn *pgx.Conn, repl *pgconn.PgConn, slot postgres.SlotActivity) error {
	const (
		retries    = 20
		retryDelay = 100 * time.Millisecond
	)

	if slot.ActivePID != nil {
		if err := postgres.TerminateSlotWalsender(slot.Name).Exec(ctx, conn); err != nil {
			return fmt.Errorf("failed to end the walsender for replication slot %s: %w", slot.Name, err)
		}
	}
	// The walsender releases the slot when it exits, shortly after it's
	// signaled, so the first attempts can still find the slot in use.
	for range retries {
		err := execReplicationCommand(ctx, repl, postgres.EnableSlotFailoverCommand(slot.Name))
		if err == nil {
			return nil
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != pgerrcode.ObjectInUse {
			return fmt.Errorf("failed to enable failover on replication slot %s: %w", slot.Name, err)
		}
		if err := utils.SleepContext(ctx, retryDelay); err != nil {
			return err
		}
	}
	// Still in use, e.g. the subscriber reconnected first. The caller lists
	// the remaining slots again and retries.
	return nil
}

func execReplicationCommand(ctx context.Context, repl *pgconn.PgConn, command string) error {
	results, err := repl.Exec(ctx, command).ReadAll()
	if err != nil {
		return err
	}
	for _, result := range results {
		if result.Err != nil {
			return result.Err
		}
	}
	return nil
}

// EnsureSpockExtensionUpdated waits for the Spock extension in the connected
// database to match the installed Spock binaries. Spock's manager worker
// updates the extension when the server starts on new binaries. If that
// hasn't happened within the timeout, this runs the update itself. It does
// nothing when Spock isn't installed in the database.
func EnsureSpockExtensionUpdated(ctx context.Context, conn *pgx.Conn, timeout time.Duration) error {
	const pollInterval = 2 * time.Second

	deadline := time.Now().Add(timeout)
	for {
		versions, err := spockExtensionVersions(ctx, conn)
		if err != nil {
			return err
		}
		if versions == nil || versions.Installed == nil {
			// Spock isn't installed in this database.
			return nil
		}
		if *versions.Installed == versions.Default {
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
		if err := utils.SleepContext(ctx, pollInterval); err != nil {
			return err
		}
	}

	if err := postgres.UpdateSpockExtension().Exec(ctx, conn); err != nil {
		return fmt.Errorf("failed to update the spock extension: %w", err)
	}
	versions, err := spockExtensionVersions(ctx, conn)
	if err != nil {
		return err
	}
	if versions != nil && versions.Installed != nil && *versions.Installed != versions.Default {
		return fmt.Errorf("the spock extension is at version %s after updating, expected %s", *versions.Installed, versions.Default)
	}
	return nil
}

func spockExtensionVersions(ctx context.Context, conn *pgx.Conn) (*postgres.ExtensionVersions, error) {
	rows, err := postgres.SpockExtensionVersions().Structs(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to get spock extension versions: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// WaitForSubscriptionsReplicating waits until every Spock subscription on the
// connected node reports the replicating status.
func WaitForSubscriptionsReplicating(ctx context.Context, conn *pgx.Conn, timeout time.Duration) error {
	const pollInterval = 2 * time.Second

	deadline := time.Now().Add(timeout)
	for {
		statuses, err := postgres.SubscriptionStatuses().Structs(ctx, conn)
		if err != nil {
			return fmt.Errorf("failed to get subscription statuses: %w", err)
		}
		var pending []string
		for _, s := range statuses {
			if s.Status != postgres.SubStatusReplicating {
				pending = append(pending, fmt.Sprintf("%s (%s)", s.Name, s.Status))
			}
		}
		if len(pending) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("subscriptions did not reach the replicating status: %s", strings.Join(pending, ", "))
		}
		if err := utils.SleepContext(ctx, pollInterval); err != nil {
			return err
		}
	}
}
