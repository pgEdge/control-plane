//go:build etcd_lifecycle_test

package etcd_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
	"github.com/pgEdge/control-plane/server/internal/testutils"
)

func TestRemoteEtcd(t *testing.T) {
	t.Parallel()

	serverA, serverB, serverC := testCluster(t)

	cfg := config.Config{
		HostID:      uuid.NewString(),
		DataDir:     t.TempDir(),
		StorageType: config.StorageTypeRemoteEtcd,
		IPv4Address: "127.0.0.1",
		Hostname:    "localhost",
		RemoteEtcd: config.RemoteEtcd{
			LogLevel: "debug",
		},
	}
	remote := etcd.NewRemoteEtcd(cfgMgr(t, cfg), testutils.Logger(t))

	join(t, serverA, remote, cfg)

	client, err := remote.GetClient()
	require.NoError(t, err)
	require.NotNil(t, client)

	ctx := context.Background()

	// Basic client operations
	_, err = client.Put(ctx, "/foo", "bar")
	require.NoError(t, err)

	resp, err := client.Get(ctx, "/foo")
	require.NoError(t, err)
	require.Equal(t, int64(1), resp.Count)
	require.Equal(t, "bar", string(resp.Kvs[0].Value))

	// Shut down one server at a time and validate that the client is still
	// operational.
	for _, server := range []*etcd.EmbeddedEtcd{serverA, serverB, serverC} {
		server.Shutdown()

		resp, err = client.Get(ctx, "/foo")
		require.NoError(t, err)
		require.Equal(t, int64(1), resp.Count)
		require.Equal(t, "bar", string(resp.Kvs[0].Value))

		err = server.Start(ctx)
		require.NoError(t, err)
	}

	// Cleanup
	require.NoError(t, client.Close())
	require.NoError(t, serverA.RemoveHost(ctx, cfg.HostID))
}

func testCluster(t testing.TB) (*etcd.EmbeddedEtcd, *etcd.EmbeddedEtcd, *etcd.EmbeddedEtcd) {
	t.Helper()

	serverA, _ := testEmbedded(t)
	serverB, cfgB := testEmbedded(t)
	serverC, cfgC := testEmbedded(t)

	ctx := t.Context()
	serverA.Start(ctx)

	join(t, serverA, serverB, cfgB)
	join(t, serverA, serverC, cfgC)

	// Important: the above test does not work with two members because etcd
	// becomes unavailable if the number of available members is less than a
	// quorum. We need to keep this in mind when planning deployment shapes.
	return serverA, serverB, serverC
}

func testEmbedded(t testing.TB) (*etcd.EmbeddedEtcd, config.Config) {
	cfg := config.Config{
		HostID:      uuid.NewString(),
		DataDir:     t.TempDir(),
		StorageType: config.StorageTypeEmbeddedEtcd,
		IPv4Address: "127.0.0.1",
		Hostname:    "localhost",
		EmbeddedEtcd: config.EmbeddedEtcd{
			ClientLogLevel: "debug",
			ServerLogLevel: "debug",
			ClientPort:     storagetest.GetFreePort(t),
			PeerPort:       storagetest.GetFreePort(t),
		},
	}
	server := etcd.NewEmbeddedEtcd(cfgMgr(t, cfg), testutils.Logger(t))

	t.Cleanup(func() {
		server.Shutdown()
	})

	return server, cfg
}

func join(t testing.TB, existing, new etcd.Etcd, newCfg config.Config) {
	t.Helper()

	ctx := t.Context()
	creds, err := existing.AddHost(ctx, etcd.HostCredentialOptions{
		HostID:              newCfg.HostID,
		Hostname:            newCfg.Hostname,
		IPv4Address:         newCfg.IPv4Address,
		EmbeddedEtcdEnabled: newCfg.StorageType == config.StorageTypeEmbeddedEtcd,
	})
	require.NoError(t, err)
	require.NotNil(t, creds)

	leader, err := existing.Leader(ctx)
	require.NoError(t, err)

	err = new.Join(ctx, etcd.JoinOptions{
		Leader:      leader,
		Credentials: creds,
	})
	require.NoError(t, err)
}
