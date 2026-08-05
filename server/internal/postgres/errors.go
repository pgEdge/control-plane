package postgres

import (
	"errors"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
)

// IsSpockNodeNotConfigured reports whether err is a PostgreSQL error
// indicating that the current database has not been initialized as a spock
// node (SQLSTATE 55000 — object_not_in_prerequisite_state, as raised by
// spock.sync_event and related functions when spock.node_create has not been
// called or the node has been dropped via spock.node_drop).
//
// Source: https://github.com/pgEdge/spock/blob/main/src/spock_functions.c
// PostgreSQL SQLSTATE reference: https://www.postgresql.org/docs/current/errcodes-appendix.html
func IsSpockNodeNotConfigured(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ObjectNotInPrerequisiteState
}

func IsDatabaseNotExists(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.InvalidCatalogName
}

// IsQueryCanceled reports whether err is a PostgreSQL error indicating the
// query was canceled (SQLSTATE 57014), which is what a caller sees when a
// context deadline fires while a query is in flight: pgx sends a cancel
// request and the server can return this error before the client observes
// ctx.Err() itself.
func IsQueryCanceled(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.QueryCanceled
}
