package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/pgEdge/control-plane/common/configuration"
	"github.com/pgEdge/control-plane/common/ds"
	"github.com/pgEdge/control-plane/common/validation"
)

const ProfileNameDefault string = "default"

type OutputType string

func (f OutputType) String() string {
	return string(f)
}

const (
	OutputTypeTable OutputType = "table"
	OutputTypeJSON  OutputType = "json"
)

func OutputTypes() ds.Set[OutputType] {
	return ds.NewSet(
		OutputTypeTable,
		OutputTypeJSON,
	)
}

var _ configuration.Config = Config{}

type Config struct {
	Profile  string             `koanf:"profile" json:"profile,omitempty" yaml:"profile,omitempty" toml:"profile,omitempty"`
	Profiles map[string]Profile `koanf:"profiles" json:"profiles,omitempty" yaml:"profiles,omitempty" toml:"profiles,omitempty"`
	Verbose  bool               `koanf:"verbose" json:"verbose,omitempty" yaml:"verbose,omitempty" toml:"verbose,omitempty"`
	Silent   bool               `koanf:"silent" json:"silent,omitempty" yaml:"silent,omitempty" toml:"silent,omitempty"`
	Pretty   bool               `koanf:"pretty" json:"pretty,omitempty" yaml:"pretty,omitempty" toml:"pretty,omitempty"`
	Output   OutputType         `koanf:"output" json:"output,omitempty" yaml:"output,omitempty" toml:"output,omitempty"`
}

func (c Config) Validate() error {
	var errs []error
	for _, key := range slices.Sorted(maps.Keys(c.Profiles)) {
		path := validation.
			NewPath("profiles").
			AppendMapKey(key)
		errs = append(errs, c.Profiles[key].validate(path)...)
	}
	if err := validation.Required(validation.NewPath("required"), c.Profile); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (c Config) SelectedProfile() Profile {
	return c.Profiles[c.Profile]
}

func DefaultConfig() (Config, error) {
	hostID, err := getShortHostname()
	if err != nil {
		return Config{}, err
	}
	return Config{
		Profile: ProfileNameDefault,
		Profiles: map[string]Profile{
			ProfileNameDefault: {
				Servers: []Server{
					{
						ID:  hostID,
						URL: "http://localhost:3000",
					},
				},
			},
		},
	}, nil
}

func DefaultSource() (*configuration.Source, error) {
	cfg, err := DefaultConfig()
	if err != nil {
		return nil, err
	}
	src, err := configuration.NewStructSource(cfg)
	if err != nil {
		return nil, err
	}
	return src, nil
}

type Profile struct {
	Servers []Server `koanf:"servers" json:"servers,omitempty" yaml:"servers,omitempty" toml:"servers,omitempty"`
	TLS     TLS      `koanf:"tls" json:"tls,omitzero" yaml:"tls,omitzero" toml:"tls,omitzero"`
}

func (p Profile) validate(path validation.Path) []error {
	var errs []error
	serversPath := path.Append("servers")
	if len(p.Servers) == 0 {
		errs = append(errs, &validation.Error{
			Path: serversPath,
			Err:  errors.New("at least one server is required"),
		})
	}
	for i, server := range p.Servers {
		errs = append(errs, server.validate(serversPath.AppendArrayIndex(i))...)
	}
	return errs
}

type Server struct {
	ID  string `koanf:"id" json:"id,omitempty" yaml:"id,omitempty" toml:"id,omitempty"`
	URL string `koanf:"url" json:"url,omitempty" yaml:"url,omitempty" toml:"url,omitempty"`
}

func (s Server) validate(path validation.Path) []error {
	var errs []error
	if err := validation.Required(path.Append("id"), s.ID); err != nil {
		errs = append(errs, err)
	}
	if err := validation.Required(path.Append("url"), s.URL); err != nil {
		errs = append(errs, err)
	}
	return errs
}

type TLS struct {
	CACert string `koanf:"ca_cert" json:"ca_cert,omitempty" yaml:"ca_cert,omitempty" toml:"ca_cert,omitempty"`
	Cert   string `koanf:"cert" json:"cert,omitempty" yaml:"cert,omitempty" toml:"cert,omitempty"`
	Key    string `koanf:"key" json:"key,omitempty" yaml:"key,omitempty" toml:"key,omitempty"`
}

func getShortHostname() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("failed to get system hostname: %w", err)
	}
	// Only return up to the first separator
	short, _, _ := strings.Cut(hostname, ".")

	return short, nil
}
