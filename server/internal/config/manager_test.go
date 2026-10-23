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

	t.Run("etcd log level does not persist in generated config", func(t *testing.T) {
		data := t.TempDir()

		// Simulate an env var / config override of the etcd log level.
		user := config.Config{
			HostID:   "test-host",
			DataDir:  data,
			EtcdMode: config.EtcdModeServer,
			EtcdServer: config.EtcdServer{
				LogLevel:   "debug",
				PeerPort:   2380,
				ClientPort: 2379,
			},
			EtcdClient: config.EtcdClient{
				LogLevel:  "debug",
				Endpoints: []string{"https://127.0.0.1:2379"},
			},
		}

		manager := config.NewManager(structSource(t, user))
		require.NoError(t, manager.Load())
		require.Equal(t, "debug", manager.Config().EtcdServer.LogLevel)

		// This mirrors what provideEtcd does: only the persistable fields are
		// written into the generated config, not the whole struct.
		appCfg := manager.Config()
		generated := manager.GeneratedConfig()
		generated.EtcdMode = appCfg.EtcdMode
		generated.EtcdServer = appCfg.EtcdServer.Persistable()
		generated.EtcdClient = appCfg.EtcdClient.Persistable()
		require.NoError(t, manager.UpdateGeneratedConfig(generated))

		// A second manager, sharing the same data dir, simulates the override
		// being removed (e.g. the env var is unset on the next restart).
		userWithoutOverride := config.Config{
			HostID:  "test-host",
			DataDir: data,
		}
		manager2 := config.NewManager(structSource(t, userWithoutOverride))
		require.NoError(t, manager2.Load())

		assert.Equal(t, "fatal", manager2.Config().EtcdServer.LogLevel, "log level should revert to default, not the stale persisted value")
		assert.Equal(t, "fatal", manager2.Config().EtcdClient.LogLevel, "log level should revert to default, not the stale persisted value")
		assert.Equal(t, 2380, manager2.Config().EtcdServer.PeerPort, "ports should still be persisted")
		assert.Equal(t, 2379, manager2.Config().EtcdServer.ClientPort, "ports should still be persisted")
		assert.Equal(t, []string{"https://127.0.0.1:2379"}, manager2.Config().EtcdClient.Endpoints, "endpoints should still be persisted")
	})

	t.Run("invalid user-specified config", func(t *testing.T) {
		user := config.Config{
			HostID: "test-host",
		}

		manager := config.NewManager(structSource(t, user))
		assert.ErrorContains(t, manager.Load(), "data_dir cannot be empty")
	})
}

func TestEtcdServerPersistable(t *testing.T) {
	server := config.EtcdServer{
		LogLevel:        "debug",
		PeerPort:        2380,
		ClientPort:      2379,
		ForceNewCluster: true,
	}

	assert.Equal(t, config.EtcdServer{
		PeerPort:   2380,
		ClientPort: 2379,
	}, server.Persistable())
}

func TestEtcdClientPersistable(t *testing.T) {
	client := config.EtcdClient{
		LogLevel:  "debug",
		Endpoints: []string{"https://127.0.0.1:2379"},
	}

	assert.Equal(t, config.EtcdClient{
		Endpoints: []string{"https://127.0.0.1:2379"},
	}, client.Persistable())
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
