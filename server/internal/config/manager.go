package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	kjson "github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Manager struct {
	sources     []*Source
	config      Config
	generated   Config
	generatedMu sync.Mutex
}

func NewManager(sources ...*Source) *Manager {
	return &Manager{
		sources: sources,
	}
}

func (m *Manager) Config() Config {
	return m.config
}

func (m *Manager) Load() error {
	defaults, err := DefaultConfig()
	if err != nil {
		return err
	}

	userK, err := m.loadUserConfig()
	if err != nil {
		return err
	}

	generatedK, err := m.loadGeneratedConfig(userK)
	if err != nil {
		return err
	}

	// Order of preference goes:
	// 1. User-specified config
	// 2. Generated config
	// 3. Defaults
	combinedK := koanf.New(".")
	if err := LoadStruct(combinedK, defaults); err != nil {
		return fmt.Errorf("failed to load defaults: %w", err)
	}
	if err := combinedK.Merge(generatedK); err != nil {
		return fmt.Errorf("failed to merge user and generated configs: %w", err)
	}
	if err := combinedK.Merge(userK); err != nil {
		return fmt.Errorf("failed to merge user and generated configs: %w", err)
	}

	var generated Config
	if err := generatedK.Unmarshal("", &generated); err != nil {
		return fmt.Errorf("failed to unmarshal combined config: %w", err)
	}

	var combined Config
	if err := combinedK.Unmarshal("", &combined); err != nil {
		return fmt.Errorf("failed to unmarshal combined config: %w", err)
	}

	if err := combined.Validate(); err != nil {
		return err
	}

	m.generated = generated
	m.config = combined

	return nil
}

func (m *Manager) GeneratedConfig() Config {
	m.generatedMu.Lock()
	defer m.generatedMu.Unlock()

	return m.generated
}

func (m *Manager) UpdateGeneratedConfig(config Config) error {
	m.generatedMu.Lock()
	defer m.generatedMu.Unlock()

	k := koanf.New(".")
	if err := LoadStruct(k, config); err != nil {
		return err
	}

	raw, err := k.Marshal(kjson.Parser())
	if err != nil {
		return fmt.Errorf("failed to marshal generated config: %w", err)
	}

	err = os.WriteFile(m.generatedPath(m.config.DataDir), raw, 0o600)
	if err != nil {
		return fmt.Errorf("failed to write generated config: %w", err)
	}

	return m.Load()
}

func (m *Manager) loadUserConfig() (*koanf.Koanf, error) {
	k := koanf.New(".")
	for _, source := range m.sources {
		err := k.Load(source.Provider(k), source.Parser, source.Options...)
		if err != nil {
			return nil, fmt.Errorf("failed to load user-specified config: %w", err)
		}
	}

	return k, nil
}

func (m *Manager) loadGeneratedConfig(user *koanf.Koanf) (*koanf.Koanf, error) {
	dataDir := user.String("data_dir")
	if dataDir == "" {
		return nil, errors.New("data_dir cannot be empty")
	}

	path := m.generatedPath(dataDir)
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return koanf.New("."), nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to check if generated config exists: %w", err)
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(path), kjson.Parser()); err != nil {
		return nil, fmt.Errorf("failed to load generated config: %w", err)
	}

	return k, nil
}

func (m *Manager) generatedPath(dataDir string) string {
	return filepath.Join(dataDir, "generated.config.json")
}
