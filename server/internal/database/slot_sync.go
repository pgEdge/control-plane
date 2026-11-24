package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/postgres"
)

// pg17VersionNum is the first server_version_num with native slot sync and
// the synced column in pg_replication_slots.
const pg17VersionNum = 170000

// SupportsSlotFailover reports whether the connected server has native slot
// sync (PostgreSQL 17 or later).
func SupportsSlotFailover(ctx context.Context, conn *pgx.Conn) (bool, error) {
	versionNum, err := postgres.ServerVersionNum().Scalar(ctx, conn)
	if err != nil {
		return false, fmt.Errorf("failed to get server version: %w", err)
	}
	return versionNum >= pg17VersionNum, nil
}

// CleanUpStaleSpockSlotCopies drops the Spock slot copies on a standby that
// native slot sync didn't create, when the standby uses native slot sync
// (Spock 6 or later on PostgreSQL 17 or later). Such copies appear when a
// former primary keeps its own slots after a switchover or failover, and
// they stop native slot sync from keeping the standby's copies current.
// spockVersion is the running Spock version. It does nothing unless the
// instance is a standby streaming from a primary.
func CleanUpStaleSpockSlotCopies(ctx context.Context, conn *pgx.Conn, spockVersion string) error {
	version, err := ds.ParseVersion(spockVersion)
	if err != nil {
		return nil
	}
	if major, ok := version.Major(); !ok || major < 6 {
		return nil
	}
	supported, err := SupportsSlotFailover(ctx, conn)
	if err != nil || !supported {
		return err
	}
	enabled, err := postgres.NativeSlotSyncEnabled().Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to check whether native slot sync is enabled: %w", err)
	}
	if !enabled {
		return nil
	}
	return dropStaleSpockSlotCopies(ctx, conn)
}

// dropStaleSpockSlotCopies drops the Spock slots on this standby that native
// slot sync didn't create. A copy that's in use is skipped and dropped on a
// later call.
func dropStaleSpockSlotCopies(ctx context.Context, conn *pgx.Conn) error {
	stale, err := postgres.StaleSpockSlotCopies().Structs(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to list stale replication slot copies: %w", err)
	}
	for _, slot := range stale {
		if slot.ActivePID != nil {
			continue
		}
		err := postgres.DropStaleSpockSlotCopy(slot.Name).Exec(ctx, conn)
		var pgErr *pgconn.PgError
		switch {
		case err == nil:
		case errors.As(err, &pgErr) && (pgErr.Code == pgerrcode.ObjectInUse || pgErr.Code == pgerrcode.UndefinedObject):
			// Picked up by a process, or already gone. Retried on the next
			// call if it's still there.
		default:
			return fmt.Errorf("failed to drop stale replication slot copy %s: %w", slot.Name, err)
		}
	}
	return nil
}
