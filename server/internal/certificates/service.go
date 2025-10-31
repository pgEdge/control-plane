package certificates

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

// TODO: this should be configurable (and much, much shorter) once we've
// implemented rotation.
var (
	certificateDuration = time.Until(time.Now().AddDate(20, 0, 0))
)

type Service struct {
	ca    *RootCA
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{
		store: store,
	}
}

func (s *Service) Start(ctx context.Context) error {
	stored, err := s.store.CA.Get().Exec(ctx)
	if err == nil {
		ca, err := StoredToRootCA(stored)
		if err != nil {
			return fmt.Errorf("failed to unmarshal stored CA: %w", err)
		}
		s.ca = ca
		return nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("failed to fetch CA: %w", err)
	}
	ca, err := CreateRootCA()
	if err != nil {
		return fmt.Errorf("failed to create CA: %w", err)
	}
	stored, err = RootCAToStored(ca)
	if err != nil {
		return fmt.Errorf("failed to marshal CA: %w", err)
	}
	if err := s.store.CA.Create(stored).Exec(ctx); err != nil {
		return fmt.Errorf("failed to store CA: %w", err)
	}
	s.ca = ca
	return nil
}

func (s *Service) CACert() []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: s.ca.Cert.Raw,
	})
}

func postgresUserID(instanceID, username string) string {
	return fmt.Sprintf("instance:%s:postgres-user:%s", instanceID, username)
}

func (s *Service) PostgresUser(ctx context.Context, instanceID, username string) (*Principal, error) {
	id := postgresUserID(instanceID, username)

	return s.getPrincipal(ctx, id, userCertTemplate(username))
}

func (s *Service) PostgresUserTLS(ctx context.Context, instanceID, hostname, username string) (*tls.Config, error) {
	id := postgresUserID(instanceID, username)

	principal, err := s.getPrincipal(ctx, id, userCertTemplate(username))
	if err != nil {
		return nil, err
	}
	clientCert, err := tls.X509KeyPair(principal.CertPEM, principal.KeyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to read client cert: %w", err)
	}
	certPool := x509.NewCertPool()
	if ok := certPool.AppendCertsFromPEM(s.CACert()); !ok {
		return nil, errors.New("failed to use CA cert")
	}

	return &tls.Config{
		RootCAs:      certPool,
		Certificates: []tls.Certificate{clientCert},
		ServerName:   hostname,
	}, nil
}

func (s *Service) RemovePostgresUser(ctx context.Context, instanceID, username string) error {
	id := postgresUserID(instanceID, username)

	return s.removePrincipal(ctx, id)
}

func instanceEtcdUserID(instanceID string) string {
	return fmt.Sprintf("instance:%s:etcd-user", instanceID)
}

func (s *Service) InstanceEtcdUser(ctx context.Context, instanceID string) (*Principal, error) {
	id := instanceEtcdUserID(instanceID)

	return s.getPrincipal(ctx, id, &x509.CertificateRequest{})
}

func (s *Service) RemoveInstanceEtcdUser(ctx context.Context, instanceID string) error {
	id := instanceEtcdUserID(instanceID)

	return s.removePrincipal(ctx, id)
}

func hostEtcdUserID(hostID string) string {
	return fmt.Sprintf("host:%s:etcd-user", hostID)
}

func (s *Service) HostEtcdUser(ctx context.Context, hostID string) (*Principal, error) {
	id := hostEtcdUserID(hostID)

	return s.getPrincipal(ctx, id, &x509.CertificateRequest{})
}

func (s *Service) RemoveHostEtcdUser(ctx context.Context, hostID string) error {
	id := hostEtcdUserID(hostID)

	return s.removePrincipal(ctx, id)
}

func postgresServerID(instanceID string) string {
	return fmt.Sprintf("instance:%s:postgres-server", instanceID)
}

func (s *Service) PostgresServer(ctx context.Context, instanceID, hostname string, dnsNames, ips []string) (*Principal, error) {
	id := postgresServerID(instanceID)

	return s.getPrincipal(ctx, id, serverCertTemplate(hostname, dnsNames, ips))
}

func (s *Service) RemovePostgresServer(ctx context.Context, instanceID string) error {
	id := postgresServerID(instanceID)

	return s.removePrincipal(ctx, id)
}

func etcdServerID(hostID string) string {
	return fmt.Sprintf("host:%s:etcd-server", hostID)
}

func (s *Service) EtcdServer(ctx context.Context, hostID, hostname string, dnsNames, ips []string) (*Principal, error) {
	id := etcdServerID(hostID)

	return s.getPrincipal(ctx, id, serverCertTemplate(hostname, dnsNames, ips))
}

func (s *Service) RemoveEtcdServer(ctx context.Context, hostID string) error {
	id := etcdServerID(hostID)

	return s.removePrincipal(ctx, id)
}

func (s *Service) JoinToken() string {
	return s.ca.JoinToken
}

func (s *Service) getPrincipal(ctx context.Context, id string, template *x509.CertificateRequest) (*Principal, error) {
	stored, err := s.store.Principal.GetByKey(id).Exec(ctx)
	if err == nil {
		principal, err := StoredToPrincipal(stored)
		if err != nil {
			return nil, err
		}
		matches, err := certPEMMatchesTemplate(principal.CertPEM, template)
		if err != nil {
			return nil, err
		}
		if matches {
			return principal, nil
		}
		// If the existing principal's cert doesn't match our template, we'll
		// recreate the cert.
	} else if !errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf("failed to fetch principal: %w", err)
	}
	// principal does not exist, create a new one
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair for new principal: %w", err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		return nil, fmt.Errorf("failed to create CSR for new principal: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: csr,
	})
	certPEM, err := s.ca.CreateSignedCertFromCSR(csrPEM, certificateDuration)
	if err != nil {
		return nil, fmt.Errorf("failed to sign certificate for new principal: %w", err)
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyBytes,
	})
	principal := &Principal{
		ID:      id,
		KeyPEM:  keyPEM,
		CertPEM: certPEM,
	}
	if err := s.store.Principal.Put(PrincipalToStored(principal)).Exec(ctx); err != nil {
		return nil, fmt.Errorf("failed to store new principal: %w", err)
	}
	return principal, nil
}

func (s *Service) removePrincipal(ctx context.Context, id string) error {
	if _, err := s.store.Principal.DeleteByKey(id).Exec(ctx); err != nil {
		return fmt.Errorf("failed to delete principal: %w", err)
	}
	return nil
}

func (s *Service) Verify(certPEM []byte) error {
	return s.ca.Verify(certPEM)
}
