package swarm

import (
	"errors"
	"net/netip"
	"path/filepath"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
)

type NetworkInfo struct {
	Name    string       `json:"name"`
	ID      string       `json:"id"`
	Subnet  netip.Prefix `json:"subnet"`
	Gateway netip.Addr   `json:"gateway"`
}

func (n NetworkInfo) Validate() []error {
	var errs []error
	if n.Name == "" {
		errs = append(errs, errors.New("name: cannot be empty"))
	}
	if n.ID == "" {
		errs = append(errs, errors.New("id: cannot be empty"))
	}
	if !n.Subnet.IsValid() {
		errs = append(errs, errors.New("subnet: invalid subnet"))
	}
	if !n.Gateway.IsValid() {
		errs = append(errs, errors.New("gateway: invalid gateway"))
	}
	return errs
}

type DataPaths struct {
	Dir string `json:"dir"`
	// PgData string `json:"pgdata"`
}

func NewDataPaths(instanceDir string) DataPaths {
	dataDir := filepath.Join(instanceDir, "data")
	return DataPaths{
		Dir: dataDir,
		// PgData: filepath.Join(dataDir, "pgdata"),
	}
}

type ConfigPaths struct {
	Dir         string `json:"dir"`
	PatroniYAML string `json:"patroni_yaml"`
}

func NewConfigPaths(instanceDir string) ConfigPaths {
	configDir := filepath.Join(instanceDir, "configs")
	return ConfigPaths{
		Dir:         configDir,
		PatroniYAML: filepath.Join(configDir, "patroni.yaml"),
	}
}

type CertificatePaths struct {
	Dir                   string `json:"dir"`
	CACert                string `json:"ca_cert"`
	ServerCert            string `json:"server_cert"`
	ServerKey             string `json:"server_key"`
	SuperuserCert         string `json:"superuser_cert"`
	SuperuserKey          string `json:"superuser_key"`
	PatroniReplicatorCert string `json:"patroni_replicator_cert"`
	PatroniReplicatorKey  string `json:"patroni_replicator_key"`
	EtcdClientCert        string `json:"etcd_client_cert"`
	EtcdClientKey         string `json:"etcd_client_key"`
}

func NewCertificatePaths(instanceDir string) CertificatePaths {
	certificateDir := filepath.Join(instanceDir, "certificates")
	return CertificatePaths{
		Dir:                   certificateDir,
		CACert:                filepath.Join(certificateDir, "ca.crt"),
		ServerCert:            filepath.Join(certificateDir, "server.crt"),
		ServerKey:             filepath.Join(certificateDir, "server.key"),
		SuperuserCert:         filepath.Join(certificateDir, "superuser.crt"),
		SuperuserKey:          filepath.Join(certificateDir, "superuser.key"),
		PatroniReplicatorCert: filepath.Join(certificateDir, "patroni-replicator.crt"),
		PatroniReplicatorKey:  filepath.Join(certificateDir, "patroni-replicator.key"),
		EtcdClientCert:        filepath.Join(certificateDir, "etcd-client.crt"),
		EtcdClientKey:         filepath.Join(certificateDir, "etcd-client.key"),
	}
}

type HostPaths struct {
	// Database              string `json:"database"`
	// Instance              string `json:"instance"`
	Data         DataPaths        `json:"data"`
	Configs      ConfigPaths      `json:"configs"`
	Certificates CertificatePaths `json:"certificates"`
	// Data                  string `json:"data"`
	// Configs               string `json:"configs"`
	// PatroniYAML           string `json:"patroni_yaml"`
	// Certificates          string `json:"certificates"`
	// CACert                string `json:"ca_cert"`
	// SuperuserCert         string `json:"superuser_cert"`
	// SuperuserKey          string `json:"superuser_key"`
	// PatroniReplicatorCert string `json:"patroni_replicator_cert"`
	// PatroniReplicatorKey  string `json:"patroni_replicator_key"`
}

func HostPathsFor(cfg config.Config, spec *database.InstanceSpec) HostPaths {
	databaseDir := filepath.Join(cfg.DataDir, spec.DatabaseID.String())
	instanceDir := filepath.Join(databaseDir, spec.InstanceID.String())

	return HostPaths{
		Data:         NewDataPaths(instanceDir),
		Configs:      NewConfigPaths(instanceDir),
		Certificates: NewCertificatePaths(instanceDir),
	}
}

func pgbackrestBackupCmd(command string, args ...string) pgbackrest.Cmd {
	return pgbackrest.Cmd{
		PgBackrestCmd: "/usr/bin/pgbackrest",
		Config:        "/opt/pgedge/configs/pgbackrest.backup.conf",
		Stanza:        "db",
		Command:       command,
		Args:          args,
	}
}
