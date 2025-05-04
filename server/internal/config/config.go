package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Logging struct {
	Level  string `koanf:"level"`
	Pretty bool   `koanf:"pretty"`
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
	Enabled   bool   `koanf:"enabled"`
	BrokerURL string `koanf:"broker_url"`
	Topic     string `koanf:"topic"`
	ClientID  string `koanf:"client_id"`
	Username  string `koanf:"username"`
	Password  string `koanf:"password"`
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
	// if m.ClientID == "" {
	// 	errs = append(errs, errors.New("client_id: cannot be empty"))
	// }
	// if m.Username == "" {
	// 	errs = append(errs, errors.New("username: cannot be empty"))
	// }
	// if m.Password == "" {
	// 	errs = append(errs, errors.New("password: cannot be empty"))
	// }
	return errs
}

type DockerSwarm struct {
	ImageRepositoryHost        string `koanf:"image_repository_host"`
	BridgeNetworksCIDR         string `koanf:"bridge_networks_cidr"`
	BridgeNetworksSubnetBits   int    `koanf:"bridge_networks_subnet_bits"`
	DatabaseNetworksCIDR       string `koanf:"database_networks_cidr"`
	DatabaseNetworksSubnetBits int    `koanf:"database_networks_subnet_bits"`
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
	ImageRepositoryHost: "public.ecr.aws/k8c8c8g7",
	// This combination gives us 256 subnets with 16 addresses each.
	BridgeNetworksCIDR:       "172.128.128.0/20",
	BridgeNetworksSubnetBits: 28,
	// This combination gives us 256 subnets with 64 addresses each.
	DatabaseNetworksCIDR:       "10.128.128.0/18",
	DatabaseNetworksSubnetBits: 26,
}

type HTTP struct {
	Enabled  bool   `koanf:"enabled"`
	BindAddr string `koanf:"bind_addr"`
	Port     int    `koanf:"port"`
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
	ClientLogLevel string `koanf:"client_log_level"`
	ServerLogLevel string `koanf:"server_log_level"`
	PeerPort       int    `koanf:"peer_port"`
	ClientPort     int    `koanf:"client_port"`
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
	ClientLogLevel: "error",
	ServerLogLevel: "error",
	PeerPort:       2380,
	ClientPort:     2379,
}

type RemoteEtcd struct {
	LogLevel  string   `koanf:"log_level"`
	Endpoints []string `koanf:"endpoints"`
}

func (r RemoteEtcd) validate() []error {
	var errs []error
	if _, err := zerolog.ParseLevel(r.LogLevel); err != nil {
		errs = append(errs, fmt.Errorf("log_level: invalid log level %q: %w", r.LogLevel, err))
	}
	if len(r.Endpoints) == 0 {
		errs = append(errs, errors.New("endpoints: cannot be empty"))
	}
	return errs
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
	TenantID               uuid.UUID    `koanf:"tenant_id"`
	ClusterID              uuid.UUID    `koanf:"cluster_id"`
	HostID                 uuid.UUID    `koanf:"host_id"`
	Orchestrator           Orchestrator `koanf:"orchestrator"`
	DataDir                string       `koanf:"data_dir"`
	StorageType            StorageType  `koanf:"storage_type"`
	IPv4Address            string       `koanf:"ipv4_address"`
	Hostname               string       `koanf:"hostname"`
	StopGracePeriodSeconds int64        `koanf:"stop_grace_period_seconds"`
	MQTT                   MQTT         `koanf:"mqtt"`
	HTTP                   HTTP         `koanf:"http"`
	Logging                Logging      `koanf:"logging"`
	EtcdKeyRoot            string       `koanf:"etcd_key_root"`
	EmbeddedEtcd           EmbeddedEtcd `koanf:"embedded_etcd"`
	RemoteEtcd             RemoteEtcd   `koanf:"remote_etcd"`
	TraefikEnabled         bool         `koanf:"traefik_enabled"`
	VectorEnabled          bool         `koanf:"vector_enabled"`
	DockerSwarm            DockerSwarm  `koanf:"docker_swarm"`
	DatabaseOwnerUID       int          `koanf:"database_owner_uid"`
}

func (c Config) Validate() error {
	var errs []error
	if c.ClusterID == uuid.Nil {
		errs = append(errs, errors.New("cluster_id cannot be empty"))
	}
	if c.HostID == uuid.Nil {
		errs = append(errs, errors.New("host_id cannot be empty"))
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

func defaultConfig() (Config, error) {
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
		DockerSwarm:            defaultDockerSwarm,
		DatabaseOwnerUID:       1020,
	}, nil
}

// GetOutboundIP returns the first outbound IP address for this host.
func getOutboundIP() (net.IP, error) {
	// Similar to 'ip route get 1'. Does not actually make a connection since
	// UDP does not have a handshake.
	conn, err := net.Dial("udp", "8.8.8.8:80")
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
