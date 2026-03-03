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
		Parser: kjson.Parser(),
	}
}

func NewEnvVarSource() (*Source, error) {
	// We use this koanf instance to infer types when we parse the env vars.
	k := koanf.New(".")
	if err := k.Load(structs.Provider(Config{}, "koanf"), nil); err != nil {
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
					return key, strings.Split(value, ",")
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
