package encryption

import "context"

type Encryptor interface {
	Encrypt(ctx context.Context, plaintext []byte) ([]byte, error)

	Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error)

	KeyID() string
}

type KeyManager interface {
	GetEncryptor(ctx context.Context) (Encryptor, error)

	RotateKey(ctx context.Context) (Encryptor, error)
}

type HostKeyPair interface {
	PublicKeyPEM() []byte

	EncryptKey(key []byte) ([]byte, error)

	DecryptKey(encryptedKey []byte) ([]byte, error)
}
