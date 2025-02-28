package database

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/pgEdge/control-plane/server/internal/postgres"
)

type ConnectionOptions struct {
	DSN *postgres.DSN
	TLS *tls.Config
}

func ConnectToInstance(ctx context.Context, opts *ConnectionOptions) (*pgx.Conn, error) {
	// connString := fmt.Sprintf("host=%s port=%d dbname=%s", opts.Host, opts.Port, opts.DBName)
	conf, err := pgx.ParseConfig(opts.DSN.String())
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}
	conf.TLSConfig = opts.TLS
	conn, err := pgx.ConnectConfig(ctx, conf)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to instance: %w", err)
	}
	return conn, nil
}
