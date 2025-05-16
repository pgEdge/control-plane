package patroni

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

type State string

const (
	StateStopping                     State = "stopping"
	StateStopped                      State = "stopped"
	StateStopFailed                   State = "stop failed"
	StateCrashed                      State = "crashed"
	StateRunning                      State = "running"
	StateStarting                     State = "starting"
	StateStartFailed                  State = "start failed"
	StateRestarting                   State = "restarting"
	StateRestartFailed                State = "restart failed"
	StateInitializingNewCluster       State = "initializing new cluster"
	StateInitDBFailed                 State = "initdb failed"
	StateRunningCustomBootstrapScript State = "running custom bootstrap script"
	StateCustomBootstrapFailed        State = "custom bootstrap failed"
	StateCreatingReplica              State = "creating replica"
	StateUnknown                      State = "unknown"
)

var errorStates = ds.NewSet(
	StateStopFailed,
	StateCrashed,
	StateStartFailed,
	StateRestartFailed,
	StateInitDBFailed,
	StateCustomBootstrapFailed,
)

func IsErrorState(state State) bool {
	return errorStates.Has(state)
}

type InstanceRole string

const (
	InstanceRoleReplica       InstanceRole = "replica"
	InstanceRolePrimary       InstanceRole = "primary"
	InstanceRoleUninitialized InstanceRole = "uninitialized"
)

type XLog struct {
	// pg_current_wal_flush_lsn() - populated if role is primary
	Location *int64 `json:"location,omitempty"`
	// pg_wal_lsn_diff(pg_last_wal_receive_lsn(), '0/0') - populated if role is
	// replica
	ReceivedLocation *int64 `json:"received_location,omitempty"`
	// pg_wal_lsn_diff(pg_last_wal_replay_lsn(), '0/0) - populated if role is
	// replica
	ReplayedLocation *int64 `json:"replayed_location,omitempty"`
	// pg_last_xact_replay_timestamp - populated if role is replica
	ReplayedTimestamp *string `json:"replayed_timestamp,omitempty"`
	// pg_is_wal_replay_paused() - populated if role is replica
	Paused *bool `json:"paused,omitempty"`
}

type Replication struct {
	// pg_stat_activity.application_name
	ApplicationName *string `json:"application_name,omitempty"`
	// pg_stat_activity.client_addr
	ClientAddr *string `json:"client_addr,omitempty"`
	// pg_stat_replication.state
	State *string `json:"state,omitempty"`
	// pg_stat_replication.sync_priority
	SyncPriority *int64 `json:"sync_priority,omitempty"`
	// pg_stat_replication.sync_state
	SyncState *string `json:"sync_state,omitempty"`
	// pg_stat_activity.usename
	Usename *string `json:"usename,omitempty"`
}

type InstanceStatus struct {
	// Postgres state
	State *State `json:"state,omitempty"`
	// pg_postmaster_start_time()
	PostmasterStartTime *string `json:"pg_postmaster_start_time,omitempty"`
	// Based on pg_is_in_recovery() output
	Role *InstanceRole `json:"role,omitempty"`
	// Postgres version without periods, e.g. '150002' for Postgres 15.2
	ServerVersion *int64 `json:"server_version,omitempty"`
	// Structure depends on role
	XLog *XLog `json:"xlog,omitempty"`
	// True if replication mode is synchronous and this is a sync standby
	SyncStandby *bool `json:"sync_standby,omitempty"`
	// True if replication mode is quorum and this is a quorum standby
	QuorumStandby *bool `json:"quorum_standby,omitempty"`
	// PostgreSQL primary node timeline
	Timeline *int64 `json:"timeline,omitempty"`
	// One entry for each replication connection
	Replication []Replication `json:"replication,omitempty"`
	// True if cluster is in maintenance mode
	Pause *bool `json:"pause,omitempty"`
	// True if cluster has no node holding the leader lock
	ClusterUnlocked *bool `json:"cluster_unlocked,omitempty"`
	// True if DCS failsafe mode is currently active
	FailsafeModeIsActive *bool `json:"failsafe_mode_is_active,omitempty"`
	// Epoch timestamp DCS was last reached by Patroni
	DCSLastSeen *int64 `json:"dcs_last_seen,omitempty"`
	// True if PostgreSQL needs to be restarted to get new configuration
	PendingRestart *bool `json:"pending_restart,omitempty"`
}

func (s *InstanceStatus) InErrorState() bool {
	return s.State != nil && IsErrorState(*s.State)
}

func (s *InstanceStatus) InRunningState() bool {
	return s.State != nil && *s.State == StateRunning
}

type ClusterRole string

const (
	ClusterRoleLeader        ClusterRole = "leader"
	ClusterRoleStandbyLeader ClusterRole = "standby_leader"
	ClusterRoleSyncStandby   ClusterRole = "sync_standby"
	ClusterRoleQuorumStandby ClusterRole = "quorum_standby"
	ClusterRoleReplica       ClusterRole = "replica"
)

type Lag int64

func (l *Lag) UnmarshalJSON(data []byte) error {
	if string(data) == `"unknown"` {
		*l = Lag(-1)
		return nil
	}
	var lag int64
	if err := json.Unmarshal(data, &lag); err != nil {
		return err
	}
	*l = Lag(lag)
	return nil
}

type ClusterMember struct {
	// The name of the host (unique in the cluster). The members list is sorted
	// by this key
	Name *string `json:"name,omitempty"`
	// The role of this member in the cluster
	Role *ClusterRole `json:"role,omitempty"`
	// The state of this member
	State *State `json:"state,omitempty"`
	// REST API URL based on restapi->connect_address configuration
	ApiUrl *string `json:"api_url,omitempty"`
	// PostgreSQL host based on postgresql->connect_address
	Host *string `json:"host,omitempty"`
	// PostgreSQL port based on postgresql->connect_address
	Port *int `json:"port,omitempty"`
	// PostgreSQL current timeline
	Timeline *int64 `json:"timeline,omitempty"`
	// True if PostgreSQL is pending to be restarted
	PendingRestart *bool `json:"pending_restart,omitempty"`
	// True if PostgreSQL is pending to be restarted
	ScheduledRestart *bool `json:"scheduled_restart,omitempty"`
	// any tags that were set for this member
	Tags map[string]any `json:"tags,omitempty"`
	// replication lag, if applicable. We set this to -1 if patroni returns the
	// lag as 'unknown'.
	Lag *Lag `json:"lag,omitempty"`
}

func (m *ClusterMember) IsLeader() bool {
	return m.Role != nil && *m.Role == ClusterRoleLeader
}

type ScheduledSwitchover struct {
	// timestamp when switchover was scheduled to occur;
	At *string `json:"at,omitempty"`
	// name of the member to be demoted;
	From *string `json:"from,omitempty"`
	// name of the member to be promoted.
	To *string `json:"to,omitempty"`
}

type ClusterState struct {
	// List of members in the cluster
	Members []ClusterMember `json:"members,omitempty"`
	// True if cluster is in maintenance mode
	Pause *bool `json:"pause,omitempty"`
	// Populated if a switchover has been scheduled
	ScheduledSwitchover *ScheduledSwitchover `json:"scheduled_switchover,omitempty"`
}

type SynchronousMode string

const (
	SynchronousModeOff    SynchronousMode = "off"
	SynchronousModeOn     SynchronousMode = "on"
	SynchronousModeQuorum SynchronousMode = "quorum"
)

type DynamicPostgreSQLConfig struct {
	// Whether or not to use pg_rewind. Defaults to false. Note that either the
	// cluster must be initialized with data page checksums (--data-checksums
	// option for initdb) and/or wal_log_hints must be set to on, or pg_rewind
	// will not work.
	UsePgRewind *bool `json:"use_pg_rewind,omitempty"`
	// Whether or not to use replication slots. Defaults to true on PostgreSQL
	// 9.4+.
	UseSlots *bool `json:"use_slots,omitempty"`
	// Configuration parameters (GUCs) for Postgres in format {max_connections:
	// 100, wal_level: "replica", max_wal_senders: 10, wal_log_hints: "on"}.
	// Many of these are required for replication to work.
	Parameters *map[string]any `json:"parameters,omitempty"`
	// List of lines that Patroni will use to generate pg_hba.conf. Patroni
	// ignores this parameter if hba_file PostgreSQL parameter is set to a
	// non-default value.
	PgHba *[]string `json:"pg_hba,omitempty"`
	// List of lines that Patroni will use to generate pg_ident.conf. Patroni
	// ignores this parameter if ident_file PostgreSQL parameter is set to a
	// non-default value.
	PgIdent *[]string `json:"pg_ident,omitempty"`
}

type DynamicStandbyClusterConfig struct {
	// An address of remote node
	Host *string `json:"host,omitempty"`
	// A port of remote node
	Port *int `json:"port,omitempty"`
	// Which slot on the remote node to use for replication. This parameter is
	// optional, the default value is derived from the instance name (see
	// function slot_name_from_member_name).
	PrimarySlotName *string `json:"primary_slot_name,omitempty"`
	// An ordered list of methods that can be used to bootstrap standby leader
	// from the remote primary, can be different from the list defined in
	// PostgreSQL
	CreateReplicaMethods *string `json:"create_replica_methods,omitempty"`
	// Command to restore WAL records from the remote primary to nodes in a
	// standby cluster, can be different from the list defined in PostgreSQL
	RestoreCommand *string `json:"restore_command,omitempty"`
	// Cleanup command for standby leader
	ArchiveCleanupCommand *string `json:"archive_cleanup_command,omitempty"`
	// How long to wait before actually apply WAL records on a standby leader
	RecoveryMinApplyDelay *string `json:"recovery_min_apply_delay,omitempty"`
}

type DynamicConfig struct {
	// The number of seconds the loop will sleep. Default value: 10, minimum
	// possible value: 1
	LoopWait *int `json:"loop_wait,omitempty"`
	// The TTL to acquire the leader lock (in seconds). Think of it as the
	// length of time before initiation of the automatic failover process.
	// Default value: 30, minimum possible value: 20
	TTL *int `json:"ttl,omitempty"`
	// Timeout for DCS and PostgreSQL operation retries (in seconds). DCS or
	// network issues shorter than this will not cause Patroni to demote the
	// leader. Default value: 10, minimum possible value: 3
	RetryTimeout *int `json:"retry_timeout,omitempty"`
	// The maximum bytes a follower may lag to be able to participate in leader
	// election.
	MaximumLagOnFailover *int64 `json:"maximum_lag_on_failover,omitempty"`
	// The maximum bytes a synchronous follower may lag before it is considered
	// as an unhealthy candidate and swapped by healthy asynchronous follower.
	// Patroni utilize the max replica lsn if there is more than one follower,
	// otherwise it will use leader’s current wal lsn. Default is -1, Patroni
	// will not take action to swap synchronous unhealthy follower when the
	// value is set to 0 or below.
	MaximumLagOnSyncNode *int64 `json:"maximum_lag_on_syncnode,omitempty"`
	// maximum number of timeline history items kept in DCS. Default value: 0.
	// When set to 0, it keeps the full history in DCS.
	MaxTimelinesHistory *int `json:"max_timelines_history,omitempty"`
	// The amount of time a primary is allowed to recover from failures before
	// failover is triggered (in seconds). Default is 300 seconds. When set to 0
	// failover is done immediately after a crash is detected if possible.
	PrimaryStartTimeout *int `json:"primary_start_timeout,omitempty"`
	// The number of seconds Patroni is allowed to wait when stopping Postgres
	// and effective only when synchronous_mode is enabled.
	PrimaryStopTimeout *int `json:"primary_stop_timeout,omitempty"`
	// Turns on synchronous replication mode.
	SynchronousMode *SynchronousMode `json:"synchronous_mode,omitempty"`
	// Prevents disabling synchronous replication if no synchronous replicas are
	// available, blocking all client writes to the primary.
	SynchronousModeStrict *bool `json:"synchronous_mode_strict,omitempty"`
	// If synchronous_mode is enabled, this parameter is used by Patroni to
	// manage the precise number of synchronous standby instances and adjusts
	// the state in DCS and the synchronous_standby_names parameter in
	// PostgreSQL as members join and leave.
	SynchronousNodeCount *int `json:"synchronous_node_count,omitempty"`
	// Enables DCS Failsafe Mode. Defaults to false.
	FailsafeMode *bool                    `json:"failsafe_mode,omitempty"`
	PostgreSQL   *DynamicPostgreSQLConfig `json:"postgresql,omitempty"`
	// If this section is defined, we want to bootstrap a standby cluster.
	StandbyCluster *DynamicStandbyClusterConfig `json:"standby_cluster,omitempty"`
	// Retention time of physical replication slots for replicas when they are
	// shut down. Default value: 30min. Set it to 0 if you want to keep the old
	// behavior (when the member key expires from DCS, the slot is immediately
	// removed).
	MemberSlotsTtl *int `json:"member_slots_ttl,omitempty"`
	// Define permanent replication slots. These slots will be preserved during
	// switchover/failover. Permanent slots that don’t exist will be created by
	// Patroni. With PostgreSQL 11 onwards permanent physical slots are created
	// on all nodes and their position is advanced every loop_wait seconds.
	Slots *map[string]Slot `json:"slots,omitempty"`
	// List of sets of replication slot properties for which Patroni should
	// ignore matching slots. This configuration/feature/etc. is useful when
	// some replication slots are managed outside of Patroni. Any subset of
	// matching properties will cause a slot to be ignored.
	IgnoreSlots *[]IgnoreSlot `json:"ignore_slots,omitempty"`
	// Stops Patroni from making changes to the cluster.
	Pause bool `json:"pause,omitempty"`
}

type Switchover struct {
	Leader      *string    `json:"leader,omitempty"`
	Candidate   *string    `json:"candidate,omitempty"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
}

type Failover struct {
	Leader    *string `json:"leader,omitempty"`
	Candidate *string `json:"candidate,omitempty"`
}

type Restart struct {
	// If set to true Patroni will restart PostgreSQL only when restart is
	// pending in order to apply some changes in the PostgreSQL config.
	RestartPending *bool `json:"restart_pending,omitempty"`
	// Perform restart only if the current role of the node matches with the
	// role from the POST request.
	Role *string `json:"role,omitempty"`
	// Perform restart only if the current version of postgres is smaller than
	// specified in the POST request, e.g. '15.2'
	PostgresVersion *string `json:"postgres_version,omitempty"`
	// How long we should wait before PostgreSQL starts accepting connections.
	// Overrides primary_start_timeout.
	Timeout *int `json:"timeout,omitempty"`
	// Timestamp with time zone, schedule the restart somewhere in the future.
	Schedule *time.Time `json:"schedule,omitempty"`
}

// type Client interface {
// 	GetInstanceStatus() (*InstanceStatus, error)
// 	GetClusterStatus() (*ClusterState, error)
// 	GetDynamicConfig() (*DynamicConfig, error)
// 	PatchDynamicConfig(config *DynamicConfig) (*DynamicConfig, error)
// 	ScheduleSwitchover(switchover *Switchover) error
// 	CancelSwitchover() error
// 	InitiateFailover(failover *Failover) error
// 	ScheduleRestart(restart *Restart) error
// 	CancelRestart() error
// 	Reload() error
// 	Reinitialize() error
// 	Liveness() error
// 	Readiness() error
// }

type Client struct {
	baseURL *url.URL
	client  *http.Client
}

func NewClient(baseURL *url.URL, client *http.Client) *Client {
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{
		baseURL: baseURL,
		client:  client,
	}
}

func (c *Client) endpoint(pathElements ...string) string {
	endpoint := &url.URL{
		Scheme: c.baseURL.Scheme,
		Host:   c.baseURL.Host,
		Path:   path.Join(pathElements...),
	}
	return endpoint.String()
}

func (c *Client) GetInstanceStatus(ctx context.Context) (*InstanceStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("patroni"), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GET /patroni request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance status: %w", err)
	}
	defer resp.Body.Close()
	if !successful(resp) {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get instance status: %d %s", resp.StatusCode, body)
	}
	var status InstanceStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode instance status: %w", err)
	}
	return &status, nil
}

func (c *Client) GetClusterStatus(ctx context.Context) (*ClusterState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("cluster"), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GET /cluster request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster status: %w", err)
	}
	defer resp.Body.Close()
	if !successful(resp) {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get cluster status: %d %s", resp.StatusCode, body)
	}
	var status ClusterState
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode cluster status: %w", err)
	}
	return &status, nil
}

func (c *Client) GetDynamicConfig(ctx context.Context) (*DynamicConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("config"), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GET /config request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get dynamic config: %w", err)
	}
	defer resp.Body.Close()
	if !successful(resp) {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get dynamic config: %d %s", resp.StatusCode, body)
	}
	var config DynamicConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode dynamic config: %w", err)
	}
	return &config, nil
}

func (c *Client) PatchDynamicConfig(ctx context.Context, config *DynamicConfig) (*DynamicConfig, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal dynamic config: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.endpoint("config"), bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create PATCH /config request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to patch dynamic config: %w", err)
	}
	defer resp.Body.Close()
	if !successful(resp) {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to patch dynamic config: %d %s", resp.StatusCode, body)
	}
	var updatedConfig DynamicConfig
	if err := json.NewDecoder(resp.Body).Decode(&updatedConfig); err != nil {
		return nil, fmt.Errorf("failed to decode updated dynamic config: %w", err)
	}
	return &updatedConfig, nil
}

func (c *Client) ScheduleSwitchover(ctx context.Context, switchover *Switchover) error {
	data, err := json.Marshal(switchover)
	if err != nil {
		return fmt.Errorf("failed to marshal switchover: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("switchover"), bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create POST /switchover request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to schedule switchover: %w", err)
	}
	defer resp.Body.Close()
	if !successful(resp) {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to schedule switchover: %d %s", resp.StatusCode, body)
	}
	return nil
}

func (c *Client) CancelSwitchover(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint("switchover"), nil)
	if err != nil {
		return fmt.Errorf("failed to create DELETE /switchover request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to cancel switchover: %w", err)
	}
	defer resp.Body.Close()
	if !successful(resp) {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to cancel switchover: %d %s", resp.StatusCode, body)
	}
	return nil
}

func (c *Client) InitiateFailover(ctx context.Context, failover *Failover) error {
	data, err := json.Marshal(failover)
	if err != nil {
		return fmt.Errorf("failed to marshal failover: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("failover"), bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create POST /failover request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to initiate failover: %w", err)
	}
	defer resp.Body.Close()
	if !successful(resp) {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to initiate failover: %d %s", resp.StatusCode, body)
	}
	return nil
}

func (c *Client) ScheduleRestart(ctx context.Context, restart *Restart) error {
	data, err := json.Marshal(restart)
	if err != nil {
		return fmt.Errorf("failed to marshal restart: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("restart"), bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create POST /restart request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to schedule restart: %w", err)
	}
	defer resp.Body.Close()
	if !successful(resp) {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to schedule restart: %d %s", resp.StatusCode, body)
	}
	return nil
}

func (c *Client) CancelRestart(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint("restart"), nil)
	if err != nil {
		return fmt.Errorf("failed to create DELETE /restart request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to cancel restart: %w", err)
	}
	defer resp.Body.Close()
	if !successful(resp) {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to cancel restart: %d %s", resp.StatusCode, body)
	}
	return nil
}

func (c *Client) Reload(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("reload"), nil)
	if err != nil {
		return fmt.Errorf("failed to create POST /reload request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reload: %w", err)
	}
	defer resp.Body.Close()
	if !successful(resp) {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to reload: %d %s", resp.StatusCode, body)
	}
	return nil
}

func (c *Client) Reinitialize(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("reinitialize"), nil)
	if err != nil {
		return fmt.Errorf("failed to create POST /reinitialize request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reinitialize: %w", err)
	}
	defer resp.Body.Close()
	if !successful(resp) {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to reinitialize: %d %s", resp.StatusCode, body)
	}
	return nil
}

func (c *Client) Liveness(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("liveness"), nil)
	if err != nil {
		return fmt.Errorf("failed to create GET /liveness request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to check liveness: %w", err)
	}
	defer resp.Body.Close()
	if !successful(resp) {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to check liveness: %d %s", resp.StatusCode, body)
	}
	return nil
}

func (c *Client) Readiness(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("readiness"), nil)
	if err != nil {
		return fmt.Errorf("failed to create GET /readiness request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to check readiness: %w", err)
	}
	defer resp.Body.Close()
	if !successful(resp) {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to check readiness: %d %s", resp.StatusCode, body)
	}
	return nil
}

func successful(resp *http.Response) bool {
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
