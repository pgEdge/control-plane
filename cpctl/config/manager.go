package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/spf13/pflag"

	"github.com/pgEdge/control-plane/client"
	"github.com/pgEdge/control-plane/common/configuration"
	"github.com/pgEdge/control-plane/common/ds"
	"github.com/pgEdge/control-plane/common/logging"
)

type Manager struct {
	configPath    string
	configFileSrc *configuration.Source
	envSrc        *configuration.Source
	flagSrc       *configuration.Source
	combined      Config
	configFile    Config
	mu            sync.RWMutex
}

// func NewManager() *Manager, error) {

// 	return &Manager{
// 		configPath:    configPath,
// 		configFileSrc: configFileSrc,
// 		envSrc:        envSrc,
// 		flagSrc:       configuration.NewPFlagSource(flags),
// 	}, nil
// }

func (m *Manager) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.combined
}

func (m *Manager) ConfigFile() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.configFile
}

func (m *Manager) Load(configPath string, flags *pflag.FlagSet) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	configFileSrc, err := configuration.NewFileSourceWithCreate(configPath)
	if err != nil {
		return err
	}
	envSrc, err := configuration.NewEnvVarSource[Config]()
	if err != nil {
		return err
	}
	m.configPath = configPath
	m.configFileSrc = configFileSrc
	m.envSrc = envSrc
	m.flagSrc = configuration.NewPFlagSource(flags)

	return m.load()
}

func (m *Manager) UpdateActiveProfile(profile Profile) error {
	cfg := m.Config()
	file := m.ConfigFile()

	if file.Profiles == nil {
		file.Profiles = make(map[string]Profile, 1)
	}
	file.Profiles[cfg.Profile] = profile

	if err := m.UpdateConfigFile(file); err != nil {
		return fmt.Errorf("failed to update profile '%s' in configuration file: %w", cfg.Profile, err)
	}

	return nil
}

func (m *Manager) UpdateConfigFile(cfg Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Converting to a koanf instance via this struct source implementation will
	// drop empty fields.
	src, err := configuration.NewStructSource(cfg)
	if err != nil {
		return err
	}
	k, err := configuration.LoadSources(src)
	if err != nil {
		return err
	}
	raw, err := k.Marshal(m.configFileSrc.Parser)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	err = os.WriteFile(m.configPath, raw, 0o600)
	if err != nil {
		return fmt.Errorf("failed to write generated config: %w", err)
	}

	return m.load()
}

func (m *Manager) Logger() zerolog.Logger {
	cfg := m.Config()
	if cfg.Silent {
		return zerolog.Nop()
	}
	level := zerolog.InfoLevel
	if cfg.Verbose {
		level = zerolog.DebugLevel
	}
	return logging.NewLogger(level, cfg.Pretty)
}

func (m *Manager) HTTPClient() (*http.Client, error) {
	profile := m.Config().SelectedProfile()
	if profile.TLS.Cert == "" {
		return &http.Client{}, nil
	}
	cert, err := tls.LoadX509KeyPair(profile.TLS.Cert, profile.TLS.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to load key pair: %w", err)
	}
	var caCertPool *x509.CertPool
	if profile.TLS.CACert != "" {
		caCert, err := os.ReadFile(profile.TLS.CACert)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA Cert: %w", err)
		}
		caCertPool = x509.NewCertPool()
		ok := caCertPool.AppendCertsFromPEM(caCert)
		if !ok {
			return nil, fmt.Errorf("failed to use CA cert")
		}
	}
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:      caCertPool,
				Certificates: []tls.Certificate{cert},
				MinVersion:   tls.VersionTLS13,
			},
		},
	}, nil
}

func (m *Manager) APIClient(overrideURLs []string) (*client.MultiServerClient, error) {
	cli, err := m.HTTPClient()
	if err != nil {
		return nil, err
	}
	servers := m.Config().SelectedProfile().Servers
	if len(overrideURLs) != 0 {
		servers = parseOverrideURLs(overrideURLs)
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("no servers configured")
	}
	if err := populateDefaultHostIDs(servers); err != nil {
		return nil, err
	}
	configs := make([]client.ServerConfig, len(servers))
	for i, server := range servers {
		u, err := url.Parse(server.URL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse url '%s' for host '%s': %w", server.URL, server.ID, err)
		}
		configs[i] = client.NewHTTPServerConfig(server.ID, u, client.WithHTTPClient(cli))
	}

	return client.NewMultiServerClient(configs...)
}

func (m *Manager) load() error {
	// Load and validate the combined configuration
	defaultSource, err := DefaultSource()
	if err != nil {
		return err
	}
	combined, err := configuration.LoadConfig[Config](
		defaultSource,
		m.configFileSrc,
		m.envSrc,
		m.flagSrc,
	)
	if err != nil {
		return err
	}
	if err := combined.Validate(); err != nil {
		return err
	}

	// Load the config file separately without validation since it may be
	// incomplete without the other config sources.
	configFile, err := configuration.LoadConfig[Config](m.configFileSrc)
	if err != nil {
		return err
	}

	m.combined = combined
	m.configFile = configFile

	return nil
}

func parseOverrideURLs(overrideURLs []string) []Server {
	servers := make([]Server, len(overrideURLs))
	for i, override := range overrideURLs {
		var hostID, urlStr string
		before, after, found := strings.Cut(override, "=")
		if found {
			hostID = before
			urlStr = after
		} else {
			urlStr = before
		}
		servers[i] = Server{
			ID:  hostID,
			URL: urlStr,
		}
	}
	return servers
	// seenHostIDs := ds.NewSet[string]()
	// servers := make([]client.ServerConfig, len(overrideURLs))
	// for i, override := range overrideURLs {
	// 	hostID, urlStr, _ := strings.Cut(override, "=")

	// 	if hostID == "" {
	// 		for hostNum := i + 1; ; hostNum++ {
	// 			h := fmt.Sprintf("host-%d", hostNum)
	// 			if !seenHostIDs.Has(h) {
	// 				hostID = h
	// 				break
	// 			}
	// 		}
	// 	}

	// 	if seenHostIDs.Has(hostID) {
	// 		return nil, fmt.Errorf("host list has duplicate host ID '%s'", hostID)
	// 	}
	// 	seenHostIDs.Add(hostID)

	// 	u, err := url.Parse(urlStr)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("invalid URL '%s': %w", urlStr, err)
	// 	}
	// 	servers[i] = client.NewHTTPServerConfig(hostID, u, client.WithHTTPClient(cli))
	// }

	// return servers, nil
}

func populateDefaultHostIDs(servers []Server) error {
	seenHostIDs := ds.NewSet[string]()
	var unpopulated []int
	// First loop to record populated IDs
	for i, server := range servers {
		hostID := server.ID
		if hostID == "" {
			unpopulated = append(unpopulated, i)
			continue
		}
		if seenHostIDs.Has(hostID) {
			return fmt.Errorf("duplicate host ID '%s' in configured servers", hostID)
		}
		seenHostIDs.Add(hostID)
	}
	// Second loop to populate missing IDs
	for _, i := range unpopulated {
		for hostNum := i + 1; ; hostNum++ {
			id := fmt.Sprintf("host-%d", hostNum)
			if !seenHostIDs.Has(id) {
				servers[i].ID = id
				break
			}
		}
	}
	return nil
}
