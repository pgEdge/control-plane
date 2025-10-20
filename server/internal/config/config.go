package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"

	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/utils"
)

func validateRequiredID(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", name)
	}
	if err := utils.ValidateID(value); err != nil {
		return fmt.Errorf("%s invalid. %w", name, err)
	}
	return nil
}

func validateOptionalID(name, value string) error {
	if value == "" {
		return nil
	}
	if err := utils.ValidateID(value); err != nil {
		return fmt.Errorf("%s invalid. %w", name, err)
	}
	return nil
}

type Logging struct {
	Level  string `koanf:"level" json:"level,omitempty"`
	Pretty bool   `koanf:"pretty" json:"pretty,omitempty"`
}

func (l Logging) validate() []error {
	var errs []error
	if _, err := zerolog.ParseLevel(l.Level); err != nil {
		errs = append(errs, fmt.Errorf("log_level: invalid log level %q: %w", l.Level, err))
	}
	return errs
}

var loggingDefault = Logging{
	Level: "info",
}

type MQTT struct {
	Enabled   bool   `koanf:"enabled" json:"enabled,omitempty"`
	BrokerURL string `koanf:"broker_url" json:"broker_url,omitempty"`
	Topic     string `koanf:"topic" json:"topic,omitempty"`
	ClientID  string `koanf:"client_id" json:"client_id,omitempty"`
	Username  string `koanf:"username" json:"username,omitempty"`
	Password  string `koanf:"password" json:"password,omitempty"`
}

func (m MQTT) validate() []error {
	if !m.Enabled {
		return nil
	}
	var errs []error
	if m.BrokerURL == "" {
		errs = append(errs, errors.New("broker_url: cannot be empty"))
	}
	if m.Topic == "" {
		errs = append(errs, errors.New("topic: cannot be empty"))
	}
	return errs
}

type DockerSwarm struct {
	ImageRepositoryHost        string `koanf:"image_repository_host" json:"image_repository_host,omitempty"`
	BridgeNetworksCIDR         string `koanf:"bridge_networks_cidr" json:"bridge_networks_cidr,omitempty"`
	BridgeNetworksSubnetBits   int    `koanf:"bridge_networks_subnet_bits" json:"bridge_networks_subnet_bits,omitempty"`
	DatabaseNetworksCIDR       string `koanf:"database_networks_cidr" json:"database_networks_cidr,omitempty"`
	DatabaseNetworksSubnetBits int    `koanf:"database_networks_subnet_bits" json:"database_networks_subnet_bits,omitempty"`
}

func (d DockerSwarm) validate() []error {
	var errs []error

	if _, err := netip.ParsePrefix(d.BridgeNetworksCIDR); err != nil {
		errs = append(errs, fmt.Errorf("bridge_networks_cidr: %w", err))
	}
	if d.BridgeNetworksSubnetBits < 1 || d.BridgeNetworksSubnetBits > 31 {
		errs = append(errs, fmt.Errorf("bridge_networks_bits: invalid subnet bits %d", d.BridgeNetworksSubnetBits))
	}
	if _, err := netip.ParsePrefix(d.DatabaseNetworksCIDR); err != nil {
		errs = append(errs, fmt.Errorf("database_networks_cidr: %w", err))
	}
	if d.DatabaseNetworksSubnetBits < 1 || d.DatabaseNetworksSubnetBits > 31 {
		errs = append(errs, fmt.Errorf("database_networks_bits: invalid subnet bits %d", d.DatabaseNetworksSubnetBits))
	}
	return errs
}

var defaultDockerSwarm = DockerSwarm{
	ImageRepositoryHost: "ghcr.io/pgedge",
	// This combination gives us 256 subnets with 16 addresses each.
	BridgeNetworksCIDR:       "172.128.128.0/20",
	BridgeNetworksSubnetBits: 28,
	// This combination gives us 256 subnets with 64 addresses each.
	DatabaseNetworksCIDR:       "10.128.128.0/18",
	DatabaseNetworksSubnetBits: 26,
}

type HTTP struct {
	Enabled    bool   `koanf:"enabled" json:"enabled,omitempty"`
	BindAddr   string `koanf:"bind_addr" json:"bind_addr,omitempty"`
	Port       int    `koanf:"port" json:"port,omitempty"`
	CACert     string `koanf:"ca_cert" json:"ca_cert,omitempty"`
	ServerCert string `koanf:"server_cert" json:"server_cert,omitempty"`
	ServerKey  string `koanf:"server_key" json:"server_key,omitempty"`
	ClientCert string `koanf:"client_cert" json:"client_cert,omitempty"`
	ClientKey  string `koanf:"client_key" json:"client_key,omitempty"`
}

func (h HTTP) validate() []error {
	if !h.Enabled {
		return nil
	}
	var errs []error
	if h.BindAddr == "" {
		errs = append(errs, errors.New("bind_addr cannot be empty"))
	}
	if h.Port == 0 {
		errs = append(errs, errors.New("port cannot be empty"))
	}
	return errs
}

var httpDefault = HTTP{
	Enabled:  true,
	BindAddr: "0.0.0.0",
	Port:     3000,
}

type EmbeddedEtcd struct {
	ClientLogLevel string `koanf:"client_log_level" json:"client_log_level,omitempty"`
	ServerLogLevel string `koanf:"server_log_level" json:"server_log_level,omitempty"`
	PeerPort       int    `koanf:"peer_port" json:"peer_port,omitempty"`
	ClientPort     int    `koanf:"client_port" json:"client_port,omitempty"`
}

func (e EmbeddedEtcd) validate() []error {
	var errs []error
	if _, err := zerolog.ParseLevel(e.ClientLogLevel); err != nil {
		errs = append(errs, fmt.Errorf("client_log_level: invalid log level %q: %w", e.ClientLogLevel, err))
	}
	if _, err := zerolog.ParseLevel(e.ServerLogLevel); err != nil {
		errs = append(errs, fmt.Errorf("server_log_level: invalid log level %q: %w", e.ServerLogLevel, err))
	}
	return errs
}

var embeddedEtcdDefault = EmbeddedEtcd{
	ClientLogLevel: "fatal",
	ServerLogLevel: "fatal",
	PeerPort:       2380,
	ClientPort:     2379,
}

type RemoteEtcd struct {
	LogLevel  string   `koanf:"log_level" json:"log_level,omitempty"`
	Endpoints []string `koanf:"endpoints" json:"endpoints,omitempty"`
}

func (r RemoteEtcd) validate() []error {
	var errs []error
	if _, err := zerolog.ParseLevel(r.LogLevel); err != nil {
		errs = append(errs, fmt.Errorf("log_level: invalid log level %q: %w", r.LogLevel, err))
	}
	return errs
}

var remoteEtcdDefault = RemoteEtcd{
	LogLevel: "fatal",
}

type Orchestrator string

const (
	OrchestratorSwarm Orchestrator = "swarm"
)

type StorageType string

const (
	StorageTypeEmbeddedEtcd StorageType = "embedded_etcd"
	StorageTypeRemoteEtcd   StorageType = "remote_etcd"
)

type Config struct {
	TenantID               string       `koanf:"tenant_id" json:"tenant_id,omitempty"`
	HostID                 string       `koanf:"host_id" json:"host_id,omitempty"`
	Orchestrator           Orchestrator `koanf:"orchestrator" json:"orchestrator,omitempty"`
	DataDir                string       `koanf:"data_dir" json:"data_dir,omitempty"`
	StorageType            StorageType  `koanf:"storage_type" json:"storage_type,omitempty"`
	IPv4Address            string       `koanf:"ipv4_address" json:"ipv4_address,omitempty"`
	Hostname               string       `koanf:"hostname" json:"hostname,omitempty"`
	StopGracePeriodSeconds int64        `koanf:"stop_grace_period_seconds" json:"stop_grace_period_seconds,omitempty"`
	MQTT                   MQTT         `koanf:"mqtt" json:"mqtt,omitzero"`
	HTTP                   HTTP         `koanf:"http" json:"http,omitzero"`
	Logging                Logging      `koanf:"logging" json:"logging,omitzero"`
	EtcdUsername           string       `koanf:"etcd_username" json:"etcd_username,omitempty"`
	EtcdPassword           string       `koanf:"etcd_password" json:"etcd_password,omitempty"`
	EtcdKeyRoot            string       `koanf:"etcd_key_root" json:"etcd_key_root,omitempty"`
	EmbeddedEtcd           EmbeddedEtcd `koanf:"embedded_etcd" json:"embedded_etcd,omitzero"`
	RemoteEtcd             RemoteEtcd   `koanf:"remote_etcd" json:"remote_etcd,omitzero"`
	TraefikEnabled         bool         `koanf:"traefik_enabled" json:"traefik_enabled,omitempty"`
	VectorEnabled          bool         `koanf:"vector_enabled" json:"vector_enabled,omitempty"`
	DockerSwarm            DockerSwarm  `koanf:"docker_swarm" json:"docker_swarm,omitzero"`
	DatabaseOwnerUID       int          `koanf:"database_owner_uid" json:"database_owner_uid,omitempty"`
	ProfilingEnabled       bool         `koanf:"profiling_enabled" json:"profiling_enabled,omitempty"`
}

func (c Config) Validate() error {
	var errs []error
	if err := validateRequiredID("host_id", c.HostID); err != nil {
		errs = append(errs, err)
	}
	if err := validateOptionalID("tenant_id", c.TenantID); err != nil {
		errs = append(errs, err)
	}
	if c.IPv4Address == "" {
		errs = append(errs, errors.New("ipv4_address cannot be empty"))
	}
	if c.DataDir == "" {
		errs = append(errs, errors.New("data_dir cannot be empty"))
	}
	for _, err := range c.HTTP.validate() {
		errs = append(errs, fmt.Errorf("http.%w", err))
	}
	for _, err := range c.MQTT.validate() {
		errs = append(errs, fmt.Errorf("mqtt.%w", err))
	}
	for _, err := range c.Logging.validate() {
		errs = append(errs, fmt.Errorf("remote_etcd.%w", err))
	}
	if c.Orchestrator != OrchestratorSwarm {
		errs = append(errs, fmt.Errorf("orchestrator: unsupported orchestrator %q", c.Orchestrator))
	}
	switch c.Orchestrator {
	case OrchestratorSwarm:
		for _, err := range c.DockerSwarm.validate() {
			errs = append(errs, fmt.Errorf("docker_swarm.%w", err))
		}
	default:
		errs = append(errs, fmt.Errorf("host_type: unsupported host type %q", c.Orchestrator))
	}
	switch c.StorageType {
	case StorageTypeEmbeddedEtcd:
		for _, err := range c.EmbeddedEtcd.validate() {
			errs = append(errs, fmt.Errorf("embedded_etcd.%w", err))
		}
	case StorageTypeRemoteEtcd:
		for _, err := range c.RemoteEtcd.validate() {
			errs = append(errs, fmt.Errorf("remote_etcd.%w", err))
		}
	default:
		errs = append(errs, fmt.Errorf("storage_type: unsupported storage type %q", c.StorageType))
	}
	return errors.Join(errs...)
}

func DefaultConfig() (Config, error) {
	ipv4Address, err := getOutboundIP()
	if err != nil {
		return Config{}, fmt.Errorf("failed to determine default ipv4_address: %w", err)
	}
	hostname, err := getHostname(ipv4Address)
	if err != nil {
		return Config{}, fmt.Errorf("failed to determine default hostname: %w", err)
	}
	return Config{
		Orchestrator:           OrchestratorSwarm,
		StorageType:            StorageTypeEmbeddedEtcd,
		Hostname:               hostname,
		IPv4Address:            ipv4Address.String(),
		Logging:                loggingDefault,
		HTTP:                   httpDefault,
		StopGracePeriodSeconds: 30,
		EmbeddedEtcd:           embeddedEtcdDefault,
		RemoteEtcd:             remoteEtcdDefault,
		DockerSwarm:            defaultDockerSwarm,
		DatabaseOwnerUID:       26,
	}, nil
}

// GetOutboundIP returns the first outbound IP address for this host.
func getOutboundIP() (net.IP, error) {
	// Similar to 'ip route get 1'. Does not actually make a connection since
	// UDP does not have a handshake.
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return net.IPv4zero, fmt.Errorf("failed to create connection: %w", err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP, nil
}

func getHostname(myIP net.IP) (string, error) {
	// Try to lookup domain names for the given IP address
	names, err := net.LookupAddr(myIP.String())
	if err == nil && len(names) > 0 {
		// FQDNs end in a dot
		return strings.TrimRight(names[0], "."), nil
	}
	// Fallback to hostname
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("failed to get system hostname: %w", err)
	}

	return hostname, nil
}
