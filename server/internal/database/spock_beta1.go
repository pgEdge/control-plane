package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/pgEdge/control-plane/server/internal/postgres"
)

// fixSpockBeta1Catalog brings the connected primary's Spock catalog up to
// date when it was created by Spock 6.0.0-beta1 and the server now runs
// beta2, and does nothing otherwise. It must run before Spock is used on
// beta2 binaries, which expect the beta2 catalog. The update runs in repair
// mode, so that Spock doesn't replicate it to nodes that may still run
// beta1. Remove at Spock 6.0.0 GA.
func fixSpockBeta1Catalog(ctx context.Context, conn *pgx.Conn) (bool, error) {
	needsFix, err := postgres.SpockBeta1CatalogNeedsFix().Scalar(ctx, conn)
	if err != nil {
		return false, fmt.Errorf("failed to check the spock catalog: %w", err)
	}
	if !needsFix {
		return false, nil
	}
	tx, err := postgres.StartRepairModeTxn(ctx, conn)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	if err := postgres.FixSpockBeta1Catalog().Exec(ctx, tx); err != nil {
		return false, fmt.Errorf("failed to update the spock 6.0.0-beta1 catalog: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit the spock catalog update: %w", err)
	}
	return true, nil
}
