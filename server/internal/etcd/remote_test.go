package etcd_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoteEtcd(t *testing.T) {
	t.Skip("Remote etcd storage is currently disabled. We'll re-enable this test when we add it back in.")

	serverA, serverB, serverC := testCluster(t)

	remote, err := etcd.NewRemoteEtcd(config.Config{
		RemoteEtcd: config.RemoteEtcd{
			Endpoints: []string{
				serverA.ClientEndpoint(),
				serverB.ClientEndpoint(),
				serverC.ClientEndpoint(),
			},
		},
	}, zerolog.New(zerolog.NewTestWriter(t)))
	assert.NoError(t, err)
	assert.NotNil(t, remote)

	client, err := remote.GetClient()
	assert.NoError(t, err)
	assert.NotNil(t, client)

	ctx := context.Background()

	// Basic client operations
	_, err = client.Put(ctx, "/foo", "bar")
	assert.NoError(t, err)

	resp, err := client.Get(ctx, "/foo")
	assert.NoError(t, err)
	assert.Equal(t, int64(1), resp.Count)
	assert.Equal(t, "bar", string(resp.Kvs[0].Value))

	// Shut down one server at a time and validate that the client is still
	// operational.
	for _, server := range []*etcd.EmbeddedEtcd{serverA, serverB, serverC} {
		server.Shutdown()

		resp, err = client.Get(ctx, "/foo")
		assert.NoError(t, err)
		assert.Equal(t, int64(1), resp.Count)
		assert.Equal(t, "bar", string(resp.Kvs[0].Value))

		err = server.Start(ctx)
		require.NoError(t, err)
	}

	// Cleanup
	assert.NoError(t, client.Close())
}

// Using the embedded server because it's convenient.
func testCluster(t testing.TB) (*etcd.EmbeddedEtcd, *etcd.EmbeddedEtcd, *etcd.EmbeddedEtcd) {
	t.Helper()

	logger := zerolog.New(zerolog.NewTestWriter(t))

	// Important: the above test does not work with two members because etcd
	// becomes unavailable if the number of available members is less than a
	// quorum. We need to keep this in mind when planning deployment shapes.
	ctx := context.Background()
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
	err := serverA.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}

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

	serverBCreds, err := serverA.AddPeerUser(ctx, etcd.HostCredentialOptions{
		HostID:      cfgB.HostID,
		Hostname:    cfgB.Hostname,
		IPv4Address: cfgB.IPv4Address,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = serverB.Join(ctx, etcd.JoinOptions{
		Peer:        serverA.AsPeer(),
		Credentials: serverBCreds,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		serverB.Shutdown()
	})

	serverCCreds, err := serverA.AddPeerUser(ctx, etcd.HostCredentialOptions{
		HostID:      cfgC.HostID,
		Hostname:    cfgC.Hostname,
		IPv4Address: cfgC.IPv4Address,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = serverC.Join(ctx, etcd.JoinOptions{
		Peer:        serverA.AsPeer(),
		Credentials: serverCCreds,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		serverC.Shutdown()
	})

	return serverA, serverB, serverC
}
