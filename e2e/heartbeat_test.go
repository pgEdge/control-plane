//go:build e2e_test

package e2e

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// HeartBeat is a helper to create a low, consistent volume of activity on a
// node to avoid replication stalls and waits.
type HeartBeat struct {
	DB       *DatabaseFixture
	NodeName string
	Username string
	Password string
	wg       sync.WaitGroup
	cancel   context.CancelFunc
	done     chan struct{}
}

// RunHeartBeat starts a heartbeat for the given node. The heartbeat is
// automatically stopped during test cleanup.
func RunHeartBeat(t testing.TB, db *DatabaseFixture, username, password, nodeName string) {
	t.Helper()

	heartbeat := &HeartBeat{
		DB:       db,
		NodeName: nodeName,
		Username: username,
		Password: password,
	}
	heartbeat.Start(t)
	t.Cleanup(func() {
		heartbeat.Stop(t)
	})
}

func (l *HeartBeat) Start(t testing.TB) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.done = make(chan struct{})
	l.wg.Go(func() {
		defer l.cancel()

		l.DB.WithConnection(ctx, l.connOpts(), t, func(conn *pgx.Conn) {
			// Create table if not exists
			l.createTable(ctx, t, conn)

			// Persist heartbeat until Stop() is called
			l.heartbeat(ctx, t, conn)
		})
	})
}

func (l *HeartBeat) Stop(t testing.TB) {
	tLogf(t, "HeartBeat: stopping heartbeat on primary instance of node %s", l.NodeName)

	close(l.done)
	l.wg.Wait()
}

func (l *HeartBeat) connOpts() ConnectionOptions {
	return ConnectionOptions{
		Matcher: And(
			WithNode(l.NodeName),
			WithRole("primary"),
		),
		Username: l.Username,
		Password: l.Password,
	}
}

func (l *HeartBeat) createTable(ctx context.Context, t testing.TB, conn *pgx.Conn) {
	sql := `CREATE TABLE IF NOT EXISTS heartbeat (
		node_name TEXT PRIMARY KEY,
		updated_at TIMESTAMPTZ
	);`

	_, err := conn.Exec(ctx, sql)
	require.NoError(t, err)
}

func (l *HeartBeat) heartbeat(ctx context.Context, t testing.TB, conn *pgx.Conn) {
	tLogf(t, "HeartBeat: starting heartbeat on primary instance of node %s", l.NodeName)

	sql := `INSERT INTO heartbeat (node_name, updated_at)
			VALUES (@node_name, now())
			ON CONFLICT (node_name)
			DO UPDATE SET updated_at = EXCLUDED.updated_at;`

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-l.done:
			return
		case <-ticker.C:
			_, err := conn.Exec(ctx, sql, pgx.NamedArgs{"node_name": l.NodeName})
			require.NoError(t, err)
		}
	}
}
