package configuration

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	kjson "github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

type Config interface {
	Validate() error
}

type Source struct {
	Provider func(k *koanf.Koanf) koanf.Provider
	Parser   koanf.Parser
	Options  []koanf.Option
}

func LoadSources(sources ...*Source) (*koanf.Koanf, error) {
	k := koanf.New(".")
	for _, source := range sources {
		err := k.Load(source.Provider(k), source.Parser, source.Options...)
		if err != nil {
			return nil, fmt.Errorf("failed to load source: %w", err)
		}
	}

	return k, nil
}

func LoadConfig[T Config](sources ...*Source) (T, error) {
	var zero T
	k, err := LoadSources(sources...)
	if err != nil {
		return zero, err
	}
	var cfg T
	if err := k.Unmarshal("", &cfg); err != nil {
		return zero, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	// if err := cfg.Validate(); err != nil {
	// 	return zero, err
	// }
	return cfg, nil
}

func NewJsonFileSource(path string) *Source {
	return &Source{
		Provider: func(_ *koanf.Koanf) koanf.Provider {
			return file.Provider(path)
		},
		Parser: kjson.Parser(),
	}
}

func NewYamlFileSource(path string) *Source {
	return &Source{
		Provider: func(_ *koanf.Koanf) koanf.Provider {
			return file.Provider(path)
		},
		Parser: NewYAMLParser(),
	}
}

func NewTomlFileSource(path string) *Source {
	return &Source{
		Provider: func(_ *koanf.Koanf) koanf.Provider {
			return file.Provider(path)
		},
		Parser: toml.Parser(),
	}
}

func NewFileSource(path string) (*Source, error) {
	switch ext := filepath.Ext(path); ext {
	case ".json":
		return NewJsonFileSource(path), nil
	case ".yaml":
		return NewYamlFileSource(path), nil
	case ".toml":
		return NewTomlFileSource(path), nil
	default:
		return nil, fmt.Errorf("unrecognized config file extension: '%s'", ext)
	}
}

func NewFileSourceWithCreate(path string) (*Source, error) {
	src, err := NewFileSource(path)
	if err != nil {
		return nil, err
	}
	// provider := &createFileProvider{
	// 	path:   path,
	// 	source: src,
	// }
	return &Source{
		Provider: func(k *koanf.Koanf) koanf.Provider {
			return newCreateFileProvider(path, src.Parser)
		},
		Parser: src.Parser,
	}, nil
}

func NewEnvVarSource[T Config]() (*Source, error) {
	var cfg T
	// We use this koanf instance to infer types when we parse the env vars.
	k := koanf.New(".")
	if err := k.Load(structs.Provider(cfg, "koanf"), nil); err != nil {
		return nil, fmt.Errorf("failed to initialize config type reference: %w", err)
	}
	return &Source{
		Provider: func(_ *koanf.Koanf) koanf.Provider {
			return env.ProviderWithValue("PGEDGE_", ".", func(key, value string) (string, any) {
				key = strings.TrimPrefix(key, "PGEDGE_")
				key = strings.ToLower(key)
				key = strings.ReplaceAll(key, "__", ".")

				switch k.Get(key).(type) {
				case []string:
					parts := strings.Split(value, ",")
					values := make([]string, 0, len(parts))
					for _, part := range parts {
						part = strings.TrimSpace(part)
						if part != "" {
							values = append(values, part)
						}
					}

					return key, values
				}

				return key, value
			})
		},
	}, nil
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

func NewStructSource[T Config](config T) (*Source, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config to json: %w", err)
	}

	return &Source{
		Provider: func(k *koanf.Koanf) koanf.Provider {
			return rawbytes.Provider(raw)
		},
		Parser: kjson.Parser(),
	}, nil
}

var _ koanf.Provider = (*createFileProvider)(nil)

type createFileProvider struct {
	path   string
	parser koanf.Parser
	// source   *Source
	// provider koanf.Provider

}

func newCreateFileProvider(path string, parser koanf.Parser) *createFileProvider {
	return &createFileProvider{
		path:   path,
		parser: parser,
	}
}

func (c *createFileProvider) ReadBytes() ([]byte, error) {
	if err := c.createIfNotExists(); err != nil {
		return nil, err
	}
	return file.Provider(c.path).ReadBytes()
}

func (c *createFileProvider) Read() (map[string]any, error) {
	if err := c.createIfNotExists(); err != nil {
		return nil, err
	}
	return file.Provider(c.path).Read()
}

func (c *createFileProvider) createIfNotExists() error {
	_, err := os.Stat(c.path)
	if err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to check if config file exists: %w", err)
	}

	parent := filepath.Dir(c.path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("failed to create parent directory '%s' for config file: %w", parent, err)
	}
	raw, err := c.parser.Marshal(map[string]any{})
	if err != nil {
		return fmt.Errorf("failed to marshal empty config: %w", err)
	}
	err = os.WriteFile(c.path, raw, 0o600)
	if err != nil {
		return fmt.Errorf("failed to write empty config: %w", err)
	}

	return nil
}

// func (c *createFileProvider) provider(k *koanf.Koanf) koanf.Provider {

// 	return c.source.Provider(k)
// }
