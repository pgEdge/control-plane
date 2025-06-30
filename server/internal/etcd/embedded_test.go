//go:build etcd_lifecycle_test

package etcd_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
	"github.com/pgEdge/control-plane/server/internal/testutils"
)

func TestEmbeddedEtcd(t *testing.T) {
	t.Run("standalone", func(t *testing.T) {
		ctx := context.Background()
		cfg := config.Config{
			HostID:      uuid.NewString(),
			DataDir:     storagetest.TempDir(t),
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

		server := etcd.NewEmbeddedEtcd(cfg, testutils.Logger(t))
		assert.NotNil(t, server)

		initialized, err := server.IsInitialized()
		assert.NoError(t, err)
		assert.False(t, initialized)

		err = server.Start(ctx)
		assert.NoError(t, err)

		initialized, err = server.IsInitialized()
		assert.NoError(t, err)
		assert.True(t, initialized)

		client, err := server.GetClient()
		assert.NotNil(t, client)
		assert.NoError(t, err)

		_, err = client.Put(ctx, "/foo", "bar")
		assert.NoError(t, err)

		resp, err := client.Get(ctx, "/foo")
		assert.NoError(t, err)
		assert.Equal(t, int64(1), resp.Count)
		assert.Equal(t, "bar", string(resp.Kvs[0].Value))

		// Stop everything
		assert.NoError(t, server.Shutdown())

		// and start it up again
		err = server.Start(ctx)
		assert.NoError(t, err)

		client, err = server.GetClient()
		assert.NotNil(t, client)
		assert.NoError(t, err)

		resp, err = client.Get(ctx, "/foo")
		assert.NoError(t, err)
		assert.Equal(t, int64(1), resp.Count)
		assert.Equal(t, "bar", string(resp.Kvs[0].Value))

		assert.NoError(t, server.Shutdown())
	})

	t.Run("cluster - leader and follower", func(t *testing.T) {
		ctx := context.Background()
		cfgA := config.Config{
			HostID:      uuid.NewString(),
			DataDir:     storagetest.TempDir(t),
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

		serverA := etcd.NewEmbeddedEtcd(cfgA, testutils.Logger(t))
		assert.NotNil(t, serverA)

		err := serverA.Start(ctx)
		assert.NoError(t, err)

		clientA, err := serverA.GetClient()
		assert.NotNil(t, clientA)
		assert.NoError(t, err)

		_, err = clientA.Put(ctx, "/foo", "bar")
		assert.NoError(t, err)

		cfgB := config.Config{
			HostID:      uuid.NewString(),
			DataDir:     storagetest.TempDir(t),
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

		serverB := etcd.NewEmbeddedEtcd(cfgB, testutils.Logger(t))
		assert.NotNil(t, serverB)

		// Generate credentials for server B
		creds, err := serverA.AddPeerUser(ctx, etcd.HostCredentialOptions{
			HostID:      cfgB.HostID,
			Hostname:    cfgB.Hostname,
			IPv4Address: cfgB.IPv4Address,
		})
		assert.NoError(t, err)
		assert.NotNil(t, creds)

		// Start server B
		err = serverB.Join(ctx, etcd.JoinOptions{
			Peer:        serverA.AsPeer(),
			Credentials: creds,
		})
		assert.NoError(t, err)

		clientB, err := serverB.GetClient()
		assert.NotNil(t, clientB)
		assert.NoError(t, err)

		// Check that B is able to read existing value from A
		resp, err := clientB.Get(ctx, "/foo")
		assert.NoError(t, err)
		assert.Equal(t, int64(1), resp.Count)
		assert.Equal(t, "bar", string(resp.Kvs[0].Value))

		// Update the value from B
		_, err = clientB.Put(ctx, "/foo", "baz")
		assert.NoError(t, err)

		// Read it back from A
		resp, err = clientA.Get(ctx, "/foo")
		assert.NoError(t, err)
		assert.Equal(t, int64(1), resp.Count)
		assert.Equal(t, "baz", string(resp.Kvs[0].Value))

		// Shut down B
		assert.NoError(t, serverB.Shutdown())

		// Start B again so we can verify it's still clustered. We can use the
		// regular Start method since this server is already initialized in the
		// cluster.
		err = serverB.Start(ctx)
		assert.NoError(t, err)

		clientB, err = serverB.GetClient()
		assert.NotNil(t, clientB)
		assert.NoError(t, err)

		// Update the value again from B
		_, err = clientB.Put(ctx, "/foo", "qux")
		assert.NoError(t, err)

		// Read it back again from A
		resp, err = clientA.Get(ctx, "/foo")
		assert.NoError(t, err)
		assert.Equal(t, int64(1), resp.Count)
		assert.Equal(t, "qux", string(resp.Kvs[0].Value))

		assert.NoError(t, serverA.Shutdown())
		assert.NoError(t, serverB.Shutdown())
	})

	t.Run("three member cluster", func(t *testing.T) {
		logger := testutils.Logger(t)
		ctx := context.Background()

		// Initialize the cluster
		cfgA := config.Config{
			HostID:      uuid.NewString(),
			DataDir:     storagetest.TempDir(t),
			StorageType: config.StorageTypeEmbeddedEtcd,
			IPv4Address: "127.0.0.1",
			Hostname:    "localhost",
			EmbeddedEtcd: config.EmbeddedEtcd{
				ClientPort: storagetest.GetFreePort(t),
				PeerPort:   storagetest.GetFreePort(t),
			},
		}
		serverA := etcd.NewEmbeddedEtcd(cfgA, logger)
		assert.NoError(t, serverA.Start(ctx))
		t.Cleanup(func() {
			serverA.Shutdown()
		})

		cfgB := config.Config{
			HostID:      uuid.NewString(),
			DataDir:     storagetest.TempDir(t),
			StorageType: config.StorageTypeEmbeddedEtcd,
			IPv4Address: "127.0.0.1",
			Hostname:    "localhost",
			EmbeddedEtcd: config.EmbeddedEtcd{
				ClientPort: storagetest.GetFreePort(t),
				PeerPort:   storagetest.GetFreePort(t),
			},
		}
		serverB := etcd.NewEmbeddedEtcd(cfgB, logger)

		cfgC := config.Config{
			HostID:      uuid.NewString(),
			DataDir:     storagetest.TempDir(t),
			StorageType: config.StorageTypeEmbeddedEtcd,
			IPv4Address: "127.0.0.1",
			Hostname:    "localhost",
			EmbeddedEtcd: config.EmbeddedEtcd{
				ClientPort: storagetest.GetFreePort(t),
				PeerPort:   storagetest.GetFreePort(t),
			},
		}
		serverC := etcd.NewEmbeddedEtcd(cfgC, logger)

		// Join server B
		serverBCreds, err := serverA.AddPeerUser(ctx, etcd.HostCredentialOptions{
			HostID:      cfgB.HostID,
			Hostname:    cfgB.Hostname,
			IPv4Address: cfgB.IPv4Address,
		})
		assert.NoError(t, err)
		err = serverB.Join(ctx, etcd.JoinOptions{
			Peer:        serverA.AsPeer(),
			Credentials: serverBCreds,
		})
		assert.NoError(t, err)
		t.Cleanup(func() {
			serverB.Shutdown()
		})

		// Join server C
		serverCCreds, err := serverA.AddPeerUser(ctx, etcd.HostCredentialOptions{
			HostID:      cfgC.HostID,
			Hostname:    cfgC.Hostname,
			IPv4Address: cfgC.IPv4Address,
		})
		assert.NoError(t, err)
		err = serverC.Join(ctx, etcd.JoinOptions{
			Peer:        serverA.AsPeer(),
			Credentials: serverCCreds,
		})
		assert.NoError(t, err)
		t.Cleanup(func() {
			serverC.Shutdown()
		})

		// Write a value from Server A
		clientA, err := serverA.GetClient()
		assert.NoError(t, err)
		t.Cleanup(func() {
			clientA.Close()
		})

		_, err = clientA.Put(ctx, "/foo", "bar")
		assert.NoError(t, err)

		// Read it back from A
		resp, err := clientA.Get(ctx, "/foo")
		assert.NoError(t, err)
		assert.Equal(t, int64(1), resp.Count)
		assert.Equal(t, "bar", string(resp.Kvs[0].Value))

		// Read it from B
		clientB, err := serverB.GetClient()
		assert.NoError(t, err)
		t.Cleanup(func() {
			clientB.Close()
		})

		resp, err = clientB.Get(ctx, "/foo")
		assert.NoError(t, err)
		assert.Equal(t, int64(1), resp.Count)
		assert.Equal(t, "bar", string(resp.Kvs[0].Value))

		// Read it from C
		clientC, err := serverC.GetClient()
		assert.NoError(t, err)
		t.Cleanup(func() {
			clientC.Close()
		})

		resp, err = clientC.Get(ctx, "/foo")
		assert.NoError(t, err)
		assert.Equal(t, int64(1), resp.Count)
		assert.Equal(t, "bar", string(resp.Kvs[0].Value))

		// Removing a non-existent peer should produce a not found error
		err = serverA.RemovePeer(ctx, uuid.NewString())
		assert.ErrorIs(t, err, etcd.ErrMemberNotFound)

		// A cluster member cannot remove itself
		err = serverA.RemovePeer(ctx, cfgA.HostID)
		assert.ErrorIs(t, err, etcd.ErrCannotRemoveSelf)

		// Remove server C
		err = serverA.RemovePeer(ctx, cfgC.HostID)
		assert.NoError(t, err)

		// Validate that the cluster member is removed
		members, err := clientA.MemberList(ctx)
		assert.NoError(t, err)
		assert.Len(t, members.Members, 2)
		memberIDToName := make(map[uint64]string, len(members.Members))
		for _, m := range members.Members {
			assert.NotEqual(t, cfgC.HostID, m.Name)
			memberIDToName[m.ID] = m.Name
		}

		// Validate that the host user is removed
		users, err := clientA.UserList(ctx)
		assert.NoError(t, err)
		assert.Len(t, members.Members, 2)
		for _, u := range users.Users {
			assert.NotContains(t, u, cfgC.HostID)
		}

		// Attempting to remove another member should produce a minimum size err
		err = serverA.RemovePeer(ctx, cfgB.HostID)
		assert.ErrorIs(t, err, etcd.ErrMinimumClusterSize)
	})
}
