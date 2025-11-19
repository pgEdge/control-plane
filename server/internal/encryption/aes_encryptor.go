package encryption

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

const (
	// AES-256 key size in bytes
	aesKeySize = 32

	// GCM nonce size in bytes (96 bits is recommended)
	nonceSize = 12

	// Version byte to support future encryption algorithm changes
	versionByte = 0x01
)

type aesGCMEncryptor struct {
	key    []byte
	keyID  string
	cipher cipher.AEAD
}

func NewAESEncryptor(key []byte) (Encryptor, error) {
	if len(key) != aesKeySize {
		return nil, fmt.Errorf("invalid key size: expected %d bytes, got %d", aesKeySize, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	hash := sha256.Sum256(key)
	keyID := base64.RawURLEncoding.EncodeToString(hash[:8])

	return &aesGCMEncryptor{
		key:    key,
		keyID:  keyID,
		cipher: gcm,
	}, nil
}

func GenerateAESKey() ([]byte, error) {
	key := make([]byte, aesKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate encryption key: %w", err)
	}
	return key, nil
}

func (e *aesGCMEncryptor) Encrypt(ctx context.Context, plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("plaintext cannot be empty")
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := e.cipher.Seal(nil, nonce, plaintext, nil)

	// Format: version + nonce + ciphertext
	result := make([]byte, 1+nonceSize+len(ciphertext))
	result[0] = versionByte
	copy(result[1:], nonce)
	copy(result[1+nonceSize:], ciphertext)

	return result, nil
}

func (e *aesGCMEncryptor) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	minSize := 1 + nonceSize + e.cipher.Overhead()
	if len(ciphertext) < minSize {
		return nil, fmt.Errorf("ciphertext too short: expected at least %d bytes, got %d", minSize, len(ciphertext))
	}

	// Check version
	version := ciphertext[0]
	if version != versionByte {
		return nil, fmt.Errorf("unsupported encryption version: %d", version)
	}

	// Extract nonce and encrypted data
	nonce := ciphertext[1 : 1+nonceSize]
	encrypted := ciphertext[1+nonceSize:]

	// Decrypt and verify authentication tag
	plaintext, err := e.cipher.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (data may be corrupted or tampered): %w", err)
	}

	return plaintext, nil
}

func (e *aesGCMEncryptor) KeyID() string {
	return e.keyID
}
