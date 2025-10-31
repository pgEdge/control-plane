package config

import (
	"encoding/json"
	"fmt"
	"strings"

	kjson "github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/providers/rawbytes"
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
		Parser: kjson.Parser(),
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

func NewStructSource(config Config) (*Source, error) {
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

func LoadStruct(k *koanf.Koanf, config Config) error {
	// Not using the structs provider because it merges unset values over top
	// of set values. Converting to JSON first lets us take advantage of the
	// omitempty behavior.
	raw, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config to json: %w", err)
	}

	if err := k.Load(rawbytes.Provider(raw), kjson.Parser()); err != nil {
		return fmt.Errorf("failed to load config from json bytes: %w", err)
	}

	return nil
}
