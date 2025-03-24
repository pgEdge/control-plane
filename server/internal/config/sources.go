package config

import (
	"fmt"
	"strings"

	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

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

func newDefaultsSource() (*Source, error) {
	defaults, err := defaultConfig()
	if err != nil {
		return nil, err
	}
	return &Source{
		Provider: func(k *koanf.Koanf) koanf.Provider {
			return structs.Provider(defaults, "koanf")
		},
	}, nil
}

func LoadSources(sources ...*Source) (Config, error) {
	defaults, err := newDefaultsSource()
	if err != nil {
		return Config{}, err
	}
	sources = append([]*Source{defaults}, sources...)

	k := koanf.New(".")
	for _, source := range sources {
		err := k.Load(source.Provider(k), source.Parser, source.Options...)
		if err != nil {
			return Config{}, fmt.Errorf("failed to load config: %w", err)
		}
	}

	var config Config
	if err := k.Unmarshal("", &config); err != nil {
		return Config{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}
