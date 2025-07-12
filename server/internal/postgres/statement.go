package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, arguments ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row
}

type IStatement interface {
	Exec(ctx context.Context, conn Executor) error
}

type Statement struct {
	SQL  string
	Args pgx.NamedArgs
}

func (s Statement) Exec(ctx context.Context, conn Executor) error {
	_, err := conn.Exec(ctx, s.SQL, s.Args)
	return err
}

type Statements []IStatement

func (s Statements) Exec(ctx context.Context, conn Executor) error {
	for _, stmt := range s {
		if err := stmt.Exec(ctx, conn); err != nil {
			return err
		}
	}
	return nil
}

type Query[T any] struct {
	SQL  string
	Args pgx.NamedArgs
}

func (q Query[T]) Row(ctx context.Context, conn Executor) (T, error) {
	var result T
	row := conn.QueryRow(ctx, q.SQL, q.Args)
	if err := row.Scan(&result); err != nil {
		return result, err
	}
	return result, nil
}

func (q Query[T]) Rows(ctx context.Context, conn Executor) ([]T, error) {
	rows, err := conn.Query(ctx, q.SQL, q.Args)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []T
	for rows.Next() {
		var result T
		if err := rows.Scan(&result); err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

type ConditionalStatement struct {
	If   Query[bool]
	Then IStatement
	Else IStatement
}

func (s ConditionalStatement) Exec(ctx context.Context, conn Executor) error {
	condition, err := s.If.Row(ctx, conn)
	if err != nil {
		return err
	}
	switch {
	case condition:
		return s.Then.Exec(ctx, conn)
	case s.Else != nil:
		return s.Else.Exec(ctx, conn)
	default:
		return nil
	}
}
