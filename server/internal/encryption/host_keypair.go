package encryption

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

const (
	rsaKeySize         = 4096
	hostKeypairDir     = "host-keypair"
	privateKeyFilename = "private.pem"
	publicKeyFilename  = "public.pem"
	privateKeyPerm     = 0600
	publicKeyPerm      = 0644
	hostKeypairDirPerm = 0700
)

type hostKeyPair struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	publicPEM  []byte
}

func LoadOrGenerateHostKeyPair(fs afero.Fs, dataDir string) (HostKeyPair, error) {
	keypairDir := filepath.Join(dataDir, hostKeypairDir)
	privateKeyPath := filepath.Join(keypairDir, privateKeyFilename)

	// Check if keypair already exists
	if exists, err := afero.Exists(fs, privateKeyPath); err != nil {
		return nil, fmt.Errorf("failed to check if private key exists: %w", err)
	} else if exists {
		// Load existing keypair
		return loadHostKeyPair(fs, keypairDir)
	}

	return generateHostKeyPair(fs, keypairDir)
}

func loadHostKeyPair(fs afero.Fs, keypairDir string) (HostKeyPair, error) {
	privateKeyPath := filepath.Join(keypairDir, privateKeyFilename)
	publicKeyPath := filepath.Join(keypairDir, publicKeyFilename)

	// Read private key
	privateKeyPEM, err := afero.ReadFile(fs, privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	// Parse private key
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return nil, fmt.Errorf("failed to decode private key PEM")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// Read public key
	publicKeyPEM, err := afero.ReadFile(fs, publicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read public key: %w", err)
	}

	publicBlock, _ := pem.Decode(publicKeyPEM)
	if publicBlock == nil || publicBlock.Type != "RSA PUBLIC KEY" {
		return nil, fmt.Errorf("failed to decode public key PEM")
	}

	publicKey, err := x509.ParsePKCS1PublicKey(publicBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	// Verify public key matches private key
	if !publicKey.Equal(&privateKey.PublicKey) {
		return nil, fmt.Errorf("public key does not match private key")
	}

	return &hostKeyPair{
		privateKey: privateKey,
		publicKey:  publicKey,
		publicPEM:  publicKeyPEM,
	}, nil
}

func generateHostKeyPair(fs afero.Fs, keypairDir string) (HostKeyPair, error) {

	privateKey, err := rsa.GenerateKey(rand.Reader, rsaKeySize)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA keypair: %w", err)
	}

	publicKey := &privateKey.PublicKey

	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	})

	publicKeyBytes := x509.MarshalPKCS1PublicKey(publicKey)
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	if err := fs.MkdirAll(keypairDir, hostKeypairDirPerm); err != nil {
		return nil, fmt.Errorf("failed to create keypair directory: %w", err)
	}

	privateKeyPath := filepath.Join(keypairDir, privateKeyFilename)
	if err := afero.WriteFile(fs, privateKeyPath, privateKeyPEM, privateKeyPerm); err != nil {
		return nil, fmt.Errorf("failed to write private key: %w", err)
	}

	publicKeyPath := filepath.Join(keypairDir, publicKeyFilename)
	if err := afero.WriteFile(fs, publicKeyPath, publicKeyPEM, publicKeyPerm); err != nil {
		return nil, fmt.Errorf("failed to write public key: %w", err)
	}

	return &hostKeyPair{
		privateKey: privateKey,
		publicKey:  publicKey,
		publicPEM:  publicKeyPEM,
	}, nil
}

func (h *hostKeyPair) PublicKeyPEM() []byte {
	return h.publicPEM
}

func (h *hostKeyPair) EncryptKey(key []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, fmt.Errorf("key cannot be empty")
	}

	if len(key) > 446 {
		return nil, fmt.Errorf("key too large to encrypt with RSA-4096: %d bytes (max 446)", len(key))
	}

	encrypted, err := rsa.EncryptOAEP(
		sha256.New(),
		rand.Reader,
		h.publicKey,
		key,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt key: %w", err)
	}

	return encrypted, nil
}

func (h *hostKeyPair) DecryptKey(encryptedKey []byte) ([]byte, error) {
	if len(encryptedKey) == 0 {
		return nil, fmt.Errorf("encrypted key cannot be empty")
	}

	decrypted, err := rsa.DecryptOAEP(
		sha256.New(),
		rand.Reader,
		h.privateKey,
		encryptedKey,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt key: %w", err)
	}

	return decrypted, nil
}

func LoadPublicKeyFromPEM(publicKeyPEM []byte) (HostKeyPair, error) {
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil || block.Type != "RSA PUBLIC KEY" {
		return nil, fmt.Errorf("failed to decode public key PEM")
	}

	publicKey, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	return &hostKeyPair{
		privateKey: nil, // No private key available
		publicKey:  publicKey,
		publicPEM:  publicKeyPEM,
	}, nil
}

func RemoveHostKeyPair(fs afero.Fs, dataDir string) error {
	keypairDir := filepath.Join(dataDir, hostKeypairDir)
	if err := fs.RemoveAll(keypairDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove host keypair: %w", err)
	}
	return nil
}
