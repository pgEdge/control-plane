package patroni

import (
	"github.com/pgEdge/control-plane/server/internal/storage"
)

func Namespace() string {
	return storage.Prefix("/", "patroni")
}

func ClusterName(databaseID, nodeName string) string {
	return databaseID + ":" + nodeName
}

func ClusterPrefix(databaseID, nodeName string) string {
	return storage.Prefix(Namespace(), ClusterName(databaseID, nodeName))
}

func MemberKey(databaseID, nodeName, instanceID string) string {
	return storage.Key(Namespace(), ClusterName(databaseID, nodeName), "members", instanceID)
}

type LogType string

const (
	LogTypePlain LogType = "plain"
	LogTypeJson  LogType = "json"
)

type LogLevel string

const (
	LogLevelDebug    LogLevel = "DEBUG"
	LogLevelInfo     LogLevel = "INFO"
	LogLevelWarning  LogLevel = "WARNING"
	LogLevelError    LogLevel = "ERROR"
	LogLevelCritical LogLevel = "CRITICAL"
)

type TracebackLevel string

const (
	TracebackLevelDebug TracebackLevel = "DEBUG"
	TracebackLevelError TracebackLevel = "ERROR"
)

type Log struct {
	Type           *LogType             `json:"type,omitempty"`
	Level          *LogLevel            `json:"level,omitempty"`
	TracebackLevel *TracebackLevel      `json:"traceback_level,omitempty"`
	Format         *[]string            `json:"format,omitempty"`
	DateFormat     *string              `json:"dateformat,omitempty"`
	StaticFields   *map[string]string   `json:"static_fields,omitempty"`
	MaxQueueSize   *int                 `json:"max_queue_size,omitempty"`
	Dir            *string              `json:"dir,omitempty"`
	Mode           *string              `json:"mode,omitempty"`
	FileNum        *int                 `json:"file_num,omitempty"`
	FileSize       *int                 `json:"file_size,omitempty"`
	Loggers        *map[string]LogLevel `json:"loggers,omitempty"`
}

type DCSPostgreSQL struct {
	UsePgRewind  *bool           `json:"use_pg_rewind,omitempty"`
	UseSlots     *bool           `json:"use_slots,omitempty"`
	RecoveryConf *map[string]any `json:"recovery_conf,omitempty"`
	Parameters   *map[string]any `json:"parameters,omitempty"`
	PgHba        *[]string       `json:"pg_hba,omitempty"`
	PgIdent      *[]string       `json:"pg_ident,omitempty"`
}

type DCSStandbyCluster struct {
	Host                  *string   `json:"host,omitempty"`
	Port                  *int      `json:"port,omitempty"`
	PrimarySlotName       *string   `json:"primary_slot_name,omitempty"`
	CreateReplicaMethods  *[]string `json:"create_replica_methods,omitempty"`
	RestoreCommand        *string   `json:"restore_command,omitempty"`
	ArchiveCleanupCommand *string   `json:"archive_cleanup_command,omitempty"`
	RecoveryMinApplyDelay *int      `json:"recovery_min_apply_delay,omitempty"`
}

type SlotType string

const (
	SlotTypePhysical SlotType = "physical"
	SlotTypeLogical  SlotType = "logical"
)

type Slot struct {
	Type     *SlotType `json:"type,omitempty"`
	Database *string   `json:"database,omitempty"`
	Plugin   *string   `json:"plugin,omitempty"`
}

type IgnoreSlot struct {
	Name     *string   `json:"name,omitempty"`
	Type     *SlotType `json:"type,omitempty"`
	Database *string   `json:"database,omitempty"`
	Plugin   *string   `json:"plugin,omitempty"`
}

type DCS struct {
	LoopWait              *int               `json:"loop_wait,omitempty"`
	TTL                   *int               `json:"ttl,omitempty"`
	RetryTimeout          *int               `json:"retry_timeout,omitempty"`
	MaximumLagOnFailover  *int64             `json:"maximum_lag_on_failover,omitempty"`
	MaximumLagOnSyncNode  *int64             `json:"maximum_lag_on_syncnode,omitempty"`
	MaxTimelinesHistory   *int               `json:"max_timelines_history,omitempty"`
	PrimaryStartTimeout   *int               `json:"primary_start_timeout,omitempty"`
	PrimaryStopTimeout    *int               `json:"primary_stop_timeout,omitempty"`
	SynchronousMode       *string            `json:"synchronous_mode,omitempty"`
	SynchronousModeStrict *bool              `json:"synchronous_mode_strict,omitempty"`
	SynchronousModeCount  *int               `json:"synchronous_mode_count,omitempty"`
	FailsafeMode          *bool              `json:"failsafe_mode,omitempty"`
	Postgresql            *DCSPostgreSQL     `json:"postgresql,omitempty"`
	StandbyCluster        *DCSStandbyCluster `json:"standby_cluster,omitempty"`
	MemberSlotsTtl        *string            `json:"member_slots_ttl,omitempty"`
	Slots                 *map[string]Slot   `json:"slots,omitempty"`
	IgnoreSlots           *[]IgnoreSlot      `json:"ignore_slots,omitempty"`
}

type BootstrapMethod string

const (
	BoostrapMethodNameInitDB   BootstrapMethod = "initdb"
	BootstrapMethodNameRestore BootstrapMethod = "restore"
)

type BootstrapMethodConf struct {
	Command                  *string         `json:"command,omitempty"`
	KeepExistingRecoveryConf *bool           `json:"keep_existing_recovery_conf,omitempty"`
	NoParams                 *bool           `json:"no_params,omitempty"`
	RecoveryConf             *map[string]any `json:"recovery_conf,omitempty"`
}

type Bootstrap struct {
	DCS           *DCS                 `json:"dcs,omitempty"`
	Method        *BootstrapMethod     `json:"method,omitempty"`
	InitDB        *[]string            `json:"initdb,omitempty"`
	PostBootstrap *string              `json:"post_bootstrap,omitempty"`
	PostInit      *string              `json:"post_init,omitempty"`
	Restore       *BootstrapMethodConf `json:"restore,omitempty"`
}

type Etcd struct {
	Host       *string   `json:"host,omitempty"`
	Hosts      *[]string `json:"hosts,omitempty"`
	UseProxies *bool     `json:"use_proxies,omitempty"`
	URL        *string   `json:"url,omitempty"`
	Proxy      *string   `json:"proxy,omitempty"`
	SRV        *string   `json:"srv,omitempty"`
	SRVSuffix  *string   `json:"srv_suffix,omitempty"`
	Protocol   *string   `json:"protocol,omitempty"`
	Username   *string   `json:"username,omitempty"`
	Password   *string   `json:"password,omitempty"`
	CACert     *string   `json:"cacert,omitempty"`
	Cert       *string   `json:"cert,omitempty"`
	Key        *string   `json:"key,omitempty"`
}

type User struct {
	Username       *string `json:"username,omitempty"`
	Password       *string `json:"password,omitempty"`
	SSLMode        *string `json:"sslmode,omitempty"`
	SSLKey         *string `json:"sslkey,omitempty"`
	SSLPassword    *string `json:"sslpassword,omitempty"`
	SSLCert        *string `json:"sslcert,omitempty"`
	SSLRootCert    *string `json:"sslrootcert,omitempty"`
	SSLCrl         *string `json:"sslcrl,omitempty"`
	SSLCrlDir      *string `json:"sslcrldir,omitempty"`
	GSSEncMode     *string `json:"gssencmode,omitempty"`
	ChannelBinding *string `json:"channel_binding,omitempty"`
}

type Authentication struct {
	Superuser   *User `json:"superuser,omitempty"`
	Replication *User `json:"replication,omitempty"`
	Rewind      *User `json:"rewind,omitempty"`
}

type Callbacks struct {
	OnReload     *string `json:"on_reload,omitempty"`
	OnRestart    *string `json:"on_restart,omitempty"`
	OnRoleChange *string `json:"on_role_change,omitempty"`
	OnStart      *string `json:"on_start,omitempty"`
	OnStop       *string `json:"on_stop,omitempty"`
}

type BinNames struct {
	PgCtl         *string `json:"pg_ctl,omitempty"`
	Initdb        *string `json:"initdb,omitempty"`
	Pgcontroldata *string `json:"pgcontroldata,omitempty"`
	PgBasebackup  *string `json:"pg_basebackup,omitempty"`
	Postgres      *string `json:"postgres,omitempty"`
	PgIsready     *string `json:"pg_isready,omitempty"`
	PgRewind      *string `json:"pg_rewind,omitempty"`
}

type PostgreSQL struct {
	// Database                               *string            `json:"database,omitempty"`
	Authentication                         *Authentication    `json:"authentication,omitempty"`
	Callbacks                              *Callbacks         `json:"callbacks,omitempty"`
	ConnectAddress                         *string            `json:"connect_address,omitempty"`
	ProxyAddress                           *string            `json:"proxy_address,omitempty"`
	CreateReplicaMethods                   *[]string          `json:"create_replica_methods,omitempty"`
	DataDir                                *string            `json:"data_dir,omitempty"`
	ConfigDir                              *string            `json:"config_dir,omitempty"`
	BinDir                                 *string            `json:"bin_dir,omitempty"`
	BinName                                *BinNames          `json:"bin_name,omitempty"`
	Listen                                 *string            `json:"listen,omitempty"`
	UseUnixSocket                          *bool              `json:"use_unix_socket,omitempty"`
	UseUnixSocketRepl                      *bool              `json:"use_unix_socket_repl,omitempty"`
	Pgpass                                 *string            `json:"pgpass,omitempty"`
	RecoveryConf                           *map[string]any    `json:"recovery_conf,omitempty"`
	CustomConf                             *string            `json:"custom_conf,omitempty"`
	Parameters                             *map[string]any    `json:"parameters,omitempty"`
	PgHba                                  *[]string          `json:"pg_hba,omitempty"`
	PgIdent                                *[]string          `json:"pg_ident,omitempty"`
	PgCtlTimeout                           *int               `json:"pg_ctl_timeout,omitempty"`
	UsePgRewind                            *bool              `json:"use_pg_rewind,omitempty"`
	RemoveDataDirectoryOnRewindFailure     *bool              `json:"remove_data_directory_on_rewind_failure,omitempty"`
	RemoveDataDirectoryOnDivergedTimelines *bool              `json:"remove_data_directory_on_diverged_timelines,omitempty"`
	ReplicaMethod                          *map[string]string `json:"replica_method,omitempty"`
	PrePromote                             *string            `json:"pre_promote,omitempty"`
	BeforeStop                             *string            `json:"before_stop,omitempty"`
	BaseBackup                             *[]any             `json:"basebackup,omitempty"`
}

type RestAPIAuthentication struct {
	Username *string `json:"username,omitempty"`
	Password *string `json:"password,omitempty"`
}

type VerifyClient string

const (
	VerifyClientNone     VerifyClient = "none"
	VerifyClientOptional VerifyClient = "optional"
	VerifyClientRequired VerifyClient = "required"
)

type RestAPI struct {
	ConnectAddress          *string                `json:"connect_address,omitempty"`
	Listen                  *string                `json:"listen,omitempty"`
	Authentication          *RestAPIAuthentication `json:"authentication,omitempty"`
	CertFile                *string                `json:"certfile,omitempty"`
	KeyFile                 *string                `json:"keyfile,omitempty"`
	KeyFilePassword         *string                `json:"keyfile_password,omitempty"`
	CAFile                  *string                `json:"cafile,omitempty"`
	Ciphers                 *string                `json:"ciphers,omitempty"`
	VerifyClient            *VerifyClient          `json:"verify_client,omitempty"`
	Allowlist               *[]string              `json:"allowlist,omitempty"`
	AllowlistIncludeMembers *bool                  `json:"allowlist_include_members,omitempty"`
	HttpExtraHeaders        *map[string]string     `json:"http_extra_headers,omitempty"`
	HttpsExtraHeaders       *map[string]string     `json:"https_extra_headers,omitempty"`
	RequestQueueSize        *int                   `json:"request_queue_size,omitempty"`
}

type CTLAuthentication struct {
	Username *string `json:"username,omitempty"`
	Password *string `json:"password,omitempty"`
}

type CTL struct {
	Authentication  *CTLAuthentication `json:"authentication,omitempty"`
	Insecure        *bool              `json:"insecure,omitempty"`
	CACert          *string            `json:"cacert,omitempty"`
	CertFile        *string            `json:"certfile,omitempty"`
	KeyFile         *string            `json:"keyfile,omitempty"`
	KeyFilePassword *string            `json:"keyfile_password,omitempty"`
}

type WatchdogMode string

const (
	WatchdogModeOff       WatchdogMode = "off"
	WatchdogModeAutomatic WatchdogMode = "automatic"
	WatchdogModeRequired  WatchdogMode = "required"
)

type Watchdog struct {
	Mode         *WatchdogMode `json:"mode,omitempty"`
	Device       *string       `json:"device,omitempty"`
	SafetyMargin *int          `json:"safety_margin,omitempty"`
}

type Tags struct {
	CloneFrom        *bool   `json:"clonefrom,omitempty"`
	NoLoadBalance    *bool   `json:"noloadbalance,omitempty"`
	ReplicateFrom    *string `json:"replicatefrom,omitempty"`
	NoSync           *bool   `json:"nosync,omitempty"`
	NoFailover       *bool   `json:"nofailover,omitempty"`
	FailoverPriority *int    `json:"failover_priority,omitempty"`
	NoStream         *bool   `json:"nostream,omitempty"`
	DatabaseID       *string `json:"database_id,omitempty"`
	Region           *string `json:"region,omitempty"`
}

type Config struct {
	Name       *string     `json:"name,omitempty"`
	Namespace  *string     `json:"namespace,omitempty"`
	Scope      *string     `json:"scope,omitempty"`
	Log        *Log        `json:"log,omitempty"`
	Bootstrap  *Bootstrap  `json:"bootstrap,omitempty"`
	Etcd3      *Etcd       `json:"etcd3,omitempty"`
	Postgresql *PostgreSQL `json:"postgresql,omitempty"`
	RestAPI    *RestAPI    `json:"restapi,omitempty"`
	CTL        *CTL        `json:"ctl,omitempty"`
	Watchdog   *Watchdog   `json:"watchdog,omitempty"`
	Tags       *Tags       `json:"tags,omitempty"`
}
