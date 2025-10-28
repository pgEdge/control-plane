package etcd

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/rs/zerolog"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func clientConfig(cfg config.Config, logger zerolog.Logger, endpoints ...string) (clientv3.Config, error) {
	zap, err := newZapLogger(logger, cfg.EtcdClient.LogLevel, "etcd_client")
	if err != nil {
		return clientv3.Config{}, fmt.Errorf("failed to initialize etcd client logger: %w", err)
	}

	tlsCfg, err := clientTLSConfig(cfg.DataDir)
	if err != nil {
		return clientv3.Config{}, err
	}

	return clientv3.Config{
		Logger:             zap,
		Endpoints:          endpoints,
		TLS:                tlsCfg,
		Username:           cfg.EtcdUsername,
		Password:           cfg.EtcdPassword,
		DialTimeout:        5 * time.Second,
		MaxCallSendMsgSize: 10 * 1024 * 1024, // 10MB
		MaxCallRecvMsgSize: 10 * 1024 * 1024, // 10MB
	}, nil
}

func clientTLSConfig(dataDir string) (*tls.Config, error) {
	clientCert, err := tls.LoadX509KeyPair(
		filepath.Join(dataDir, "certificates", "etcd-user.crt"),
		filepath.Join(dataDir, "certificates", "etcd-user.key"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to read client cert: %w", err)
	}
	rootCA, err := os.ReadFile(filepath.Join(dataDir, "certificates", "ca.crt"))
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert: %w", err)
	}
	certPool := x509.NewCertPool()
	if ok := certPool.AppendCertsFromPEM(rootCA); !ok {
		return nil, errors.New("failed to use CA cert")
	}

	return &tls.Config{
		RootCAs:      certPool,
		Certificates: []tls.Certificate{clientCert},
		MinVersion:   tls.VersionTLS13,
	}, nil
}
