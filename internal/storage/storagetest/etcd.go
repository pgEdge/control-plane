package storagetest

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"

	"github.com/pgEdge/control-plane/internal/storage"
)

type EtcdTestServer struct {
	t         testing.TB
	etcd      *embed.Etcd
	dir       string
	clientURL string
}

func (s *EtcdTestServer) Client() storage.EtcdClient {
	client, err := clientv3.New(clientv3.Config{

		Endpoints:   []string{s.clientURL},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		s.t.Fatal(err)
	}
	return client
}

func (s *EtcdTestServer) Close() {
	s.etcd.Close()

	if s.dir != "" {
		if err := os.RemoveAll(s.dir); err != nil {
			fmt.Printf("failed to remove data dir %q: %v\n", s.dir, err)
		}
	}
}

func NewEtcdTestServer(t testing.TB) *EtcdTestServer {
	t.Helper()

	dir, err := os.MkdirTemp(os.TempDir(), "etcd-test-server")
	if err != nil {
		t.Fatal(err)
	}

	clientPort, err := getFreePort()
	if err != nil {
		t.Fatal(err)
	}
	clientURL := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%d", clientPort),
	}

	peerPort, err := getFreePort()
	if err != nil {
		t.Fatal(err)
	}
	peerURL := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%d", peerPort),
	}

	// TODO: It would be nicer to use sockets here than open a bunch of ports.
	// 		 Revisit after https://github.com/etcd-io/etcd/issues/17443 is fixed
	// 		 for real.
	// clientURL := url.URL{
	// 	Scheme: "unix",
	// 	Path:   filepath.Join(dir, "client.sock"),
	// }
	// peerURL := url.URL{
	// 	Scheme: "unix",
	// 	Path:   filepath.Join(dir, "peer.sock"),
	// }

	cfg := embed.NewConfig()
	cfg.LogLevel = "error"
	cfg.Name = "test"
	cfg.Dir = filepath.Join(dir, "data")
	cfg.ListenClientUrls = []url.URL{clientURL}
	cfg.AdvertiseClientUrls = []url.URL{clientURL}
	cfg.ListenPeerUrls = []url.URL{peerURL}
	cfg.AdvertisePeerUrls = []url.URL{peerURL}
	cfg.InitialCluster = fmt.Sprintf("test=%s", peerURL.String())
	cfg.MaxTxnOps = 2048

	e, err := embed.StartEtcd(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Blocks until ready
	select {
	case <-e.Server.ReadyNotify():
		return &EtcdTestServer{
			t:         t,
			etcd:      e,
			dir:       dir,
			clientURL: clientURL.String(),
		}
	case <-time.After(60 * time.Second):
		e.Server.Stop() // trigger a shutdown
		t.Fatal("Server took too long to start!")
	}

	return nil
}

func getFreePort() (port int, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close()
			return l.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return
}
