package certificates

import (
	"encoding/base64"
	"fmt"
	"path"

	"github.com/pgEdge/control-plane/server/internal/storage"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type Principal struct {
	ID      string
	KeyPEM  []byte
	CertPEM []byte
}

type StoredPrincipal struct {
	storage.StoredValue
	ID      string `json:"id"`
	KeyPEM  string `json:"key_pem"`
	CertPEM string `json:"cert_pem"`
}

type PrincipalStore struct {
	client *clientv3.Client
	root   string
}

func NewPrincipalStore(client *clientv3.Client, root string) *PrincipalStore {
	return &PrincipalStore{
		client: client,
		root:   root,
	}
}

func (s *PrincipalStore) Prefix() string {
	return path.Join("/", s.root, "principals")
}

func (s *PrincipalStore) Key(certificateID string) string {
	return path.Join(s.Prefix(), certificateID)
}

func (s *PrincipalStore) ExistsByKey(certificateID string) storage.ExistsOp {
	key := s.Key(certificateID)
	return storage.NewExistsOp(s.client, key)
}

func (s *PrincipalStore) GetByKey(certificateID string) storage.GetOp[*StoredPrincipal] {
	key := s.Key(certificateID)
	return storage.NewGetOp[*StoredPrincipal](s.client, key)
}

func (s *PrincipalStore) Put(item *StoredPrincipal) storage.PutOp[*StoredPrincipal] {
	key := s.Key(item.ID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *PrincipalStore) DeleteByKey(certificateID string) storage.DeleteOp {
	key := s.Key(certificateID)
	return storage.NewDeleteKeyOp(s.client, key)
}

func PrincipalToStored(p *Principal) *StoredPrincipal {
	return &StoredPrincipal{
		ID:      p.ID,
		KeyPEM:  base64.StdEncoding.EncodeToString(p.KeyPEM),
		CertPEM: base64.StdEncoding.EncodeToString(p.CertPEM),
	}
}

func StoredToPrincipal(p *StoredPrincipal) (*Principal, error) {
	keyPEM, err := base64.StdEncoding.DecodeString(p.KeyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key PEM: %w", err)
	}
	certPEM, err := base64.StdEncoding.DecodeString(p.CertPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to decode cert PEM: %w", err)
	}
	return &Principal{
		ID:      p.ID,
		KeyPEM:  keyPEM,
		CertPEM: certPEM,
	}, nil
}
