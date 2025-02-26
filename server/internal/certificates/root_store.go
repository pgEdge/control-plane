package certificates

import (
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"path"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

type StoredCA struct {
	storage.StoredValue
	KeyPEM    string `json:"key_pem"`
	CertPEM   string `json:"cert_pem"`
	JoinToken string `json:"join_token"`
}

type CAStore struct {
	client storage.EtcdClient
	root   string
}

func NewCAStore(client storage.EtcdClient, root string) *CAStore {
	return &CAStore{
		client: client,
		root:   root,
	}
}

func (s *CAStore) Key() string {
	return path.Join("/", s.root, "root_ca")
}

func (s *CAStore) Get() storage.GetOp[*StoredCA] {
	key := s.Key()
	return storage.NewGetOp[*StoredCA](s.client, key)
}

func (s *CAStore) Create(item *StoredCA) storage.PutOp[*StoredCA] {
	key := s.Key()
	return storage.NewCreateOp(s.client, key, item)
}

func (s *CAStore) Update(item *StoredCA) storage.PutOp[*StoredCA] {
	key := s.Key()
	return storage.NewUpdateOp(s.client, key, item)
}

func RootCAToStored(ca *RootCA) (*StoredCA, error) {
	keyBytes, err := x509.MarshalPKCS8PrivateKey(ca.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyBytes,
	})
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: ca.Cert.Raw,
	})

	return &StoredCA{
		KeyPEM:    base64.StdEncoding.EncodeToString(keyPEM),
		CertPEM:   base64.StdEncoding.EncodeToString(certPEM),
		JoinToken: ca.JoinToken,
	}, nil
}

func StoredToRootCA(ca *StoredCA) (*RootCA, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(ca.KeyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key PEM: %w", err)
	}
	keyPEM, _ := pem.Decode(keyBytes)
	certBytes, err := base64.StdEncoding.DecodeString(ca.CertPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to decode cert PEM: %w", err)
	}
	certPEM, _ := pem.Decode(certBytes)
	key, err := x509.ParsePKCS8PrivateKey(keyPEM.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse key: %w", err)
	}
	cert, err := x509.ParseCertificate(certPEM.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cert: %w", err)
	}

	return &RootCA{
		Cert:      cert,
		Key:       key.(crypto.Signer),
		JoinToken: ca.JoinToken,
	}, nil
}
