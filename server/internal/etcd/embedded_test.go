//go:build etcd_lifecycle_test

package etcd_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
	"github.com/pgEdge/control-plane/server/internal/testutils"
)

func TestEmbeddedEtcd(t *testing.T) {
	t.Parallel()

	t.Run("standalone", func(t *testing.T) {
		ctx := t.Context()
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
		require.NotNil(t, server)

		initialized, err := server.IsInitialized()
		require.NoError(t, err)
		require.False(t, initialized)

		err = server.Start(ctx)
		require.NoError(t, err)

		initialized, err = server.IsInitialized()
		require.NoError(t, err)
		require.True(t, initialized)

		client, err := server.GetClient()
		require.NotNil(t, client)
		require.NoError(t, err)

		_, err = client.Put(ctx, "/foo", "bar")
		require.NoError(t, err)

		resp, err := client.Get(ctx, "/foo")
		require.NoError(t, err)
		require.Equal(t, int64(1), resp.Count)
		require.Equal(t, "bar", string(resp.Kvs[0].Value))

		// Stop everything
		require.NoError(t, server.Shutdown())

		// and start it up again
		err = server.Start(ctx)
		require.NoError(t, err)

		client, err = server.GetClient()
		require.NotNil(t, client)
		require.NoError(t, err)

		resp, err = client.Get(ctx, "/foo")
		require.NoError(t, err)
		require.Equal(t, int64(1), resp.Count)
		require.Equal(t, "bar", string(resp.Kvs[0].Value))

		require.NoError(t, server.Shutdown())
	})

	t.Run("cluster - leader and follower", func(t *testing.T) {
		ctx := context.Background()
		cfgA := config.Config{
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

		serverA := etcd.NewEmbeddedEtcd(cfgMgr(t, cfgA), testutils.Logger(t))
		require.NotNil(t, serverA)

		err := serverA.Start(ctx)
		require.NoError(t, err)

		clientA, err := serverA.GetClient()
		require.NotNil(t, clientA)
		require.NoError(t, err)

		_, err = clientA.Put(ctx, "/foo", "bar")
		require.NoError(t, err)

		cfgB := config.Config{
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

		serverB := etcd.NewEmbeddedEtcd(cfgMgr(t, cfgB), testutils.Logger(t))
		require.NotNil(t, serverB)

		// Generate credentials for server B
		creds, err := serverA.AddHost(ctx, etcd.HostCredentialOptions{
			HostID:              cfgB.HostID,
			Hostname:            cfgB.Hostname,
			IPv4Address:         cfgB.IPv4Address,
			EmbeddedEtcdEnabled: true,
		})
		require.NoError(t, err)
		require.NotNil(t, creds)

		leader, err := serverA.Leader(ctx)
		require.NoError(t, err)

		// Start server B
		err = serverB.Join(ctx, etcd.JoinOptions{
			Leader:      leader,
			Credentials: creds,
		})
		require.NoError(t, err)

		clientB, err := serverB.GetClient()
		require.NotNil(t, clientB)
		require.NoError(t, err)

		// Check that B is able to read existing value from A
		resp, err := clientB.Get(ctx, "/foo")
		require.NoError(t, err)
		require.Equal(t, int64(1), resp.Count)
		require.Equal(t, "bar", string(resp.Kvs[0].Value))

		// Update the value from B
		_, err = clientB.Put(ctx, "/foo", "baz")
		require.NoError(t, err)

		// Read it back from A
		resp, err = clientA.Get(ctx, "/foo")
		require.NoError(t, err)
		require.Equal(t, int64(1), resp.Count)
		require.Equal(t, "baz", string(resp.Kvs[0].Value))

		// Shut down B
		require.NoError(t, serverB.Shutdown())

		// Start B again so we can verify it's still clustered. We can use the
		// regular Start method since this server is already initialized in the
		// cluster.
		err = serverB.Start(ctx)
		require.NoError(t, err)

		clientB, err = serverB.GetClient()
		require.NotNil(t, clientB)
		require.NoError(t, err)

		// Update the value again from B
		_, err = clientB.Put(ctx, "/foo", "qux")
		require.NoError(t, err)

		// Read it back again from A
		resp, err = clientA.Get(ctx, "/foo")
		require.NoError(t, err)
		require.Equal(t, int64(1), resp.Count)
		require.Equal(t, "qux", string(resp.Kvs[0].Value))

		// Assert that server B returns the same leader as A
		leaderA, err := serverA.Leader(ctx)
		require.NoError(t, err)
		leaderB, err := serverB.Leader(ctx)
		require.NoError(t, err)

		assert.Equal(t, leaderA, leaderB)

		require.NoError(t, serverA.Shutdown())
		require.NoError(t, serverB.Shutdown())
	})

	t.Run("three member cluster", func(t *testing.T) {
		logger := testutils.Logger(t)
		ctx := context.Background()

		// Initialize the cluster
		cfgA := config.Config{
			HostID:      uuid.NewString(),
			DataDir:     t.TempDir(),
			StorageType: config.StorageTypeEmbeddedEtcd,
			IPv4Address: "127.0.0.1",
			Hostname:    "localhost",
			EmbeddedEtcd: config.EmbeddedEtcd{
				ClientPort: storagetest.GetFreePort(t),
				PeerPort:   storagetest.GetFreePort(t),
			},
		}
		serverA := etcd.NewEmbeddedEtcd(cfgMgr(t, cfgA), logger)
		require.NoError(t, serverA.Start(ctx))
		t.Cleanup(func() {
			serverA.Shutdown()
		})

		cfgB := config.Config{
			HostID:      uuid.NewString(),
			DataDir:     t.TempDir(),
			StorageType: config.StorageTypeEmbeddedEtcd,
			IPv4Address: "127.0.0.1",
			Hostname:    "localhost",
			EmbeddedEtcd: config.EmbeddedEtcd{
				ClientPort: storagetest.GetFreePort(t),
				PeerPort:   storagetest.GetFreePort(t),
			},
		}
		serverB := etcd.NewEmbeddedEtcd(cfgMgr(t, cfgB), logger)

		cfgC := config.Config{
			HostID:      uuid.NewString(),
			DataDir:     t.TempDir(),
			StorageType: config.StorageTypeEmbeddedEtcd,
			IPv4Address: "127.0.0.1",
			Hostname:    "localhost",
			EmbeddedEtcd: config.EmbeddedEtcd{
				ClientPort: storagetest.GetFreePort(t),
				PeerPort:   storagetest.GetFreePort(t),
			},
		}
		serverC := etcd.NewEmbeddedEtcd(cfgMgr(t, cfgC), logger)

		leader, err := serverA.Leader(ctx)
		require.NoError(t, err)

		// Join server B
		serverBCreds, err := serverA.AddHost(ctx, etcd.HostCredentialOptions{
			HostID:              cfgB.HostID,
			Hostname:            cfgB.Hostname,
			IPv4Address:         cfgB.IPv4Address,
			EmbeddedEtcdEnabled: true,
		})
		require.NoError(t, err)
		err = serverB.Join(ctx, etcd.JoinOptions{
			Leader:      leader,
			Credentials: serverBCreds,
		})
		require.NoError(t, err)
		t.Cleanup(func() {
			serverB.Shutdown()
		})

		// Join server C
		serverCCreds, err := serverA.AddHost(ctx, etcd.HostCredentialOptions{
			HostID:              cfgC.HostID,
			Hostname:            cfgC.Hostname,
			IPv4Address:         cfgC.IPv4Address,
			EmbeddedEtcdEnabled: true,
		})
		require.NoError(t, err)
		err = serverC.Join(ctx, etcd.JoinOptions{
			Leader:      leader,
			Credentials: serverCCreds,
		})
		require.NoError(t, err)
		t.Cleanup(func() {
			serverC.Shutdown()
		})

		// Write a value from Server A
		clientA, err := serverA.GetClient()
		require.NoError(t, err)
		t.Cleanup(func() {
			clientA.Close()
		})

		_, err = clientA.Put(ctx, "/foo", "bar")
		require.NoError(t, err)

		// Read it back from A
		resp, err := clientA.Get(ctx, "/foo")
		require.NoError(t, err)
		require.Equal(t, int64(1), resp.Count)
		require.Equal(t, "bar", string(resp.Kvs[0].Value))

		// Read it from B
		clientB, err := serverB.GetClient()
		require.NoError(t, err)
		t.Cleanup(func() {
			clientB.Close()
		})

		resp, err = clientB.Get(ctx, "/foo")
		require.NoError(t, err)
		require.Equal(t, int64(1), resp.Count)
		require.Equal(t, "bar", string(resp.Kvs[0].Value))

		// Read it from C
		clientC, err := serverC.GetClient()
		require.NoError(t, err)
		t.Cleanup(func() {
			clientC.Close()
		})

		resp, err = clientC.Get(ctx, "/foo")
		require.NoError(t, err)
		require.Equal(t, int64(1), resp.Count)
		require.Equal(t, "bar", string(resp.Kvs[0].Value))

		// Removing a non-existent peer should not produce an error
		err = serverA.RemoveHost(ctx, uuid.NewString())
		require.NoError(t, err)

		// A cluster member cannot remove itself
		err = serverA.RemoveHost(ctx, cfgA.HostID)
		require.ErrorIs(t, err, etcd.ErrCannotRemoveSelf)

		// Remove server C
		err = serverA.RemoveHost(ctx, cfgC.HostID)
		require.NoError(t, err)

		// Validate that the cluster member is removed
		members, err := clientA.MemberList(ctx)
		require.NoError(t, err)
		require.Len(t, members.Members, 2)
		memberIDToName := make(map[uint64]string, len(members.Members))
		for _, m := range members.Members {
			require.NotEqual(t, cfgC.HostID, m.Name)
			memberIDToName[m.ID] = m.Name
		}

		// Validate that the host user is removed
		users, err := clientA.UserList(ctx)
		require.NoError(t, err)
		require.Len(t, members.Members, 2)
		for _, u := range users.Users {
			require.NotContains(t, u, cfgC.HostID)
		}

		// Attempting to remove another member should produce a minimum size err
		err = serverA.RemoveHost(ctx, cfgB.HostID)
		require.ErrorIs(t, err, etcd.ErrMinimumClusterSize)
	})
}

func cfgMgr(t testing.TB, cfg config.Config) *config.Manager {
	t.Helper()

	src, err := config.NewStructSource(cfg)
	require.NoError(t, err)

	mgr := config.NewManager(src)
	require.NoError(t, mgr.Load())

	return mgr
}
