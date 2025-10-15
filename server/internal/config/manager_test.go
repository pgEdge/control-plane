package config_test

import (
	"testing"

	"github.com/knadh/koanf/v2"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager(t *testing.T) {
	t.Run("with generated config", func(t *testing.T) {
		data := t.TempDir()
		user := config.Config{
			StopGracePeriodSeconds: 60,
			ClusterID:              "test-cluster",
			HostID:                 "test-host",
			DataDir:                data,
		}

		userSrc, err := config.NewStructSource(user)
		require.NoError(t, err)

		manager := config.NewManager(userSrc)

		require.NoError(t, manager.Load())
		assert.Equal(t, defaultWithOverrides(t, user), manager.Config())

		err = manager.UpdateGeneratedConfig(config.Config{
			StopGracePeriodSeconds: 45,
			MQTT: config.MQTT{
				Enabled:   true,
				BrokerURL: "tls://localhost:8333",
				Topic:     "/cmd/control-plane/host-1",
			},
		})
		require.NoError(t, err)

		assert.Equal(t, defaultWithOverrides(t, config.Config{
			// This value is in both the user-specified config and the generated
			// config, but the user-specified config takes precedence.
			StopGracePeriodSeconds: 60,
			ClusterID:              "test-cluster",
			HostID:                 "test-host",
			DataDir:                data,
			// This value comes from the generated config
			MQTT: config.MQTT{
				Enabled:   true,
				BrokerURL: "tls://localhost:8333",
				Topic:     "/cmd/control-plane/host-1",
			},
		}), manager.Config())
	})

	t.Run("invalid user-specified config", func(t *testing.T) {
		user := config.Config{
			ClusterID: "test-cluster",
			HostID:    "test-host",
		}

		manager := config.NewManager(structSource(t, user))
		assert.ErrorContains(t, manager.Load(), "data_dir cannot be empty")
	})
}

func structSource(t *testing.T, cfg config.Config) *config.Source {
	t.Helper()

	source, err := config.NewStructSource(cfg)
	require.NoError(t, err)

	return source
}

func defaultWithOverrides(t *testing.T, overrides config.Config) config.Config {
	t.Helper()

	defaults, err := config.DefaultConfig()
	require.NoError(t, err)

	k := koanf.New(".")
	require.NoError(t, config.LoadStruct(k, defaults))
	require.NoError(t, config.LoadStruct(k, overrides))

	var merged config.Config
	require.NoError(t, k.Unmarshal("", &merged))

	return merged
}
