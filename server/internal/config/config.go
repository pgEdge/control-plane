package config

import (
	"fmt"
	"strings"
	"sync"

	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

type Logging struct {
	Level  string `koanf:"level"`
	Pretty bool   `koanf:"pretty"`
}

var loggingDefault = Logging{
	Level: "debug",
}

type MQTT struct {
	Enabled   bool   `koanf:"enabled"`
	BrokerURL string `koanf:"broker_url"`
	Topic     string `koanf:"topic"`
	ClientID  string `koanf:"client_id"`
	Username  string `koanf:"username"`
	Password  string `koanf:"password"`
}

type DockerSwarm struct {
	DataDir        string `koanf:"data_dir"`
	TraefikEnabled bool   `koanf:"traefik_enabled"`
	VectorEnabled  bool   `koanf:"vector_enabled"`
}

type HTTP struct {
	Enabled  bool   `koanf:"enabled"`
	HostPort string `koanf:"host_port"`
}

var httpDefault = HTTP{
	Enabled:  true,
	HostPort: ":3000",
}

type EmbeddedEtcd struct {
	Enabled             bool   `koanf:"enabled"`
	DataDir             string `koanf:"data_dir"`
	PeerHostPort        string `koanf:"peer_host_port"`
	ClientHostPort      string `koanf:"client_host_port"`
	InitialClusterToken string `koanf:"initial_cluster_token"`
	InitialCluster      string `koanf:"initial_cluster"`
}

type Config struct {
	TenantID               string      `koanf:"tenant_id"`
	ClusterID              string      `koanf:"cluster_id"`
	HostID                 string      `koanf:"host_id"`
	Region                 string      `koanf:"region"`
	IPAddress              string      `koanf:"ip_address"`
	StopGracePeriodSeconds int64       `koanf:"stop_grace_period_seconds"`
	MQTT                   MQTT        `koanf:"mqtt"`
	DockerSwarm            DockerSwarm `koanf:"docker_swarm"`
	HTTP                   HTTP        `koanf:"http"`
	Logging                Logging     `koanf:"logging"`
}

var defaultConfig = Config{
	Logging:                loggingDefault,
	HTTP:                   httpDefault,
	StopGracePeriodSeconds: 30,
}

type Source struct {
	Provider func(k *koanf.Koanf) koanf.Provider
	Parser   koanf.Parser
	Options  []koanf.Option
}

func NewJsonFileSource(path string) *Source {
	return &Source{
		Provider: func(_ *koanf.Koanf) koanf.Provider {
			return file.Provider(path)
		},
		Parser: json.Parser(),
	}
}

func NewEnvVarSource() *Source {
	return &Source{
		Provider: func(_ *koanf.Koanf) koanf.Provider {
			return env.Provider("PGEDGE_", ".", func(s string) string {
				s = strings.TrimPrefix(s, "PGEDGE_")
				s = strings.ToLower(s)
				return strings.ReplaceAll(s, "__", ".")
			})
		},
	}
}

func NewPFlagSource(flagSet *pflag.FlagSet) *Source {
	return &Source{
		Provider: func(k *koanf.Koanf) koanf.Provider {
			return posflag.ProviderWithFlag(flagSet, ".", k, func(f *pflag.Flag) (string, interface{}) {
				key := strings.ReplaceAll(f.Name, "-", "_")
				return key, f.Value
			})
		},
	}
}

func newDefaultsSource() *Source {
	return &Source{
		Provider: func(k *koanf.Koanf) koanf.Provider {
			return structs.Provider(defaultConfig, "koanf")
		},
	}
}

type Manager struct {
	mu      sync.Mutex
	k       *koanf.Koanf
	sources []*Source
	config  Config
}

func NewManager(sources ...*Source) (*Manager, error) {
	m := &Manager{
		sources: append([]*Source{newDefaultsSource()}, sources...),
	}
	if err := m.Reload(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := koanf.New(".")
	for _, source := range m.sources {
		err := k.Load(source.Provider(k), source.Parser, source.Options...)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	}

	var config Config
	if err := k.Unmarshal("", &config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	m.k = k
	m.config = config

	return nil
}

func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.config
}
