package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

type Statement struct {
	SQL  string
	Args pgx.NamedArgs
}

func (s Statement) Exec(ctx context.Context, conn Executor) (pgconn.CommandTag, error) {
	return conn.Exec(ctx, s.SQL, s.Args)
}

type Statements []Statement

func (s Statements) Exec(ctx context.Context, conn Executor) error {
	for _, stmt := range s {
		_, err := stmt.Exec(ctx, conn)
		if err != nil {
			return err
		}
	}
	return nil
}
