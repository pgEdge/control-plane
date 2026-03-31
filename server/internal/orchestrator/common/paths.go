package common

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
)

type InstancePaths struct {
	Instance       Paths  `json:"instance"`
	Host           Paths  `json:"host"`
	PgBackRestPath string `json:"pg_backrest_path"`
	PatroniPath    string `json:"patroni_path"`
}

func (p *InstancePaths) HostMvDataToRestoreCmd() []string {
	return []string{"mv", p.Host.PgData(), p.Host.PgDataRestore()}
}

func (p *InstancePaths) InstanceMvRestoreToDataCmd() []string {
	return []string{"mv", p.Instance.PgDataRestore(), p.Instance.PgData()}
}

func (p *InstancePaths) PgBackRestBackupCmd(command string, args ...string) pgbackrest.Cmd {
	return pgbackrest.Cmd{
		PgBackrestCmd: p.PgBackRestPath,
		Config:        p.Instance.PgBackRestConfig(PgBackRestConfigTypeBackup),
		Stanza:        "db",
		Command:       command,
		Args:          args,
	}
}

var targetActionRestoreTypes = ds.NewSet(
	"immediate",
	"lsn",
	"name",
	"time",
	"xid",
)

func (p *InstancePaths) PgBackRestRestoreCmd(command string, args ...string) pgbackrest.Cmd {
	var hasTargetAction, needsTargetAction bool
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--target-action") {
			hasTargetAction = true
			continue // no further checks needed for this flag
		}
		var restoreType string
		if arg == "--type" && i+1 < len(args) {
			restoreType = args[i+1]
			i++ // skip the next arg since it's the value of --type
		} else if strings.HasPrefix(arg, "--type=") {
			restoreType = strings.TrimPrefix(arg, "--type=")
		} else {
			continue
		}
		if targetActionRestoreTypes.Has(restoreType) {
			needsTargetAction = true
		}
	}
	if needsTargetAction && !hasTargetAction {
		args = append(args, "--target-action=promote")
	}

	return pgbackrest.Cmd{
		PgBackrestCmd: p.PgBackRestPath,
		Config:        p.Instance.PgBackRestConfig(PgBackRestConfigTypeRestore),
		Stanza:        "db",
		Command:       command,
		Args:          args,
	}
}

type Paths struct {
	BaseDir string `json:"base_dir"`
}

func (p *Paths) Data() string {
	return filepath.Join(p.BaseDir, "data")
}

func (p *Paths) Configs() string {
	return filepath.Join(p.BaseDir, "configs")
}

func (p *Paths) Certificates() string {
	return filepath.Join(p.BaseDir, "certificates")
}

func (p *Paths) PgData() string {
	return filepath.Join(p.Data(), "pgdata")
}

func (p *Paths) PgDataRestore() string {
	return filepath.Join(p.Data(), "pgdata-restore")
}

func (p *Paths) PatroniConfig() string {
	return filepath.Join(p.Configs(), "patroni.yaml")
}

func (p *Paths) PgBackRestConfig(confType PgBackRestConfigType) string {
	return filepath.Join(p.Configs(), fmt.Sprintf("pgbackrest.%s.conf", confType))
}

func (p *Paths) EtcdCertificates() string {
	return filepath.Join(p.Certificates(), "etcd")
}

func (p *Paths) EtcdCaCert() string {
	return filepath.Join(p.EtcdCertificates(), etcdCaCertName)
}

func (p *Paths) EtcdClientCert() string {
	return filepath.Join(p.EtcdCertificates(), etcdClientCertName)
}

func (p *Paths) EtcdClientKey() string {
	return filepath.Join(p.EtcdCertificates(), etcdClientKeyName)
}

func (p *Paths) PostgresCertificates() string {
	return filepath.Join(p.Certificates(), "postgres")
}

func (p *Paths) PostgresCaCert() string {
	return filepath.Join(p.PostgresCertificates(), postgresCaCertName)
}

func (p *Paths) PostgresServerCert() string {
	return filepath.Join(p.PostgresCertificates(), postgresServerCertName)
}

func (p *Paths) PostgresServerKey() string {
	return filepath.Join(p.PostgresCertificates(), postgresServerKeyName)
}

func (p *Paths) PostgresSuperuserCert() string {
	return filepath.Join(p.PostgresCertificates(), postgresSuperuserCertName)
}

func (p *Paths) PostgresSuperuserKey() string {
	return filepath.Join(p.PostgresCertificates(), postgresSuperuserKeyName)
}

func (p *Paths) PostgresReplicatorCert() string {
	return filepath.Join(p.PostgresCertificates(), postgresReplicatorCertName)
}

func (p *Paths) PostgresReplicatorKey() string {
	return filepath.Join(p.PostgresCertificates(), postgresReplicatorKeyName)
}
