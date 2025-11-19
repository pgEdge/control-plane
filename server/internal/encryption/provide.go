package encryption

import (
	"crypto/rand"
	"fmt"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/samber/do"
	"github.com/spf13/afero"

	"github.com/pgEdge/control-plane/server/internal/config"
)

func Provide(i *do.Injector) {
	provideHostKeyPair(i)
	provideEncryptor(i)
}

func provideHostKeyPair(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (HostKeyPair, error) {
		cfg, err := do.Invoke[config.Config](i)
		if err != nil {
			return nil, err
		}
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, err
		}
		// hard coded path
		keyPairPath := filepath.Join(cfg.DataDir, "host-keypair")
		logger.Info().Str("path", keyPairPath).Msg("initializing host keypair")

		hostKeyPair, err := LoadOrGenerateHostKeyPair(afero.NewOsFs(), keyPairPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load or generate host keypair: %w", err)
		}

		logger.Info().Msg("host keypair initialized successfully")
		return hostKeyPair, nil
	})
}

func provideEncryptor(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (Encryptor, error) {
		logger, err := do.Invoke[zerolog.Logger](i)
		if err != nil {
			return nil, err
		}

		key := make([]byte, 32) // 32 bytes for AES-256
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("failed to generate encryption key: %w", err)
		}

		logger.Warn().Msg("using randomly generated encryption key - data will only be accessible by this server instance")
		logger.Info().Msg("initializing AES-256-GCM encryptor")

		encryptor, err := NewAESEncryptor(key)
		if err != nil {
			return nil, fmt.Errorf("failed to create encryptor: %w", err)
		}

		logger.Info().
			Str("key_id", encryptor.KeyID()).
			Msg("encryptor initialized successfully")

		return encryptor, nil
	})
}
