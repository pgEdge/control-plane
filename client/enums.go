package client

const (
	BackupTypeFull = "full"
	BackupTypeDiff = "diff"
	BackupTypeIncr = "incr"
)

const (
	ClusterStateAvailable = "available"
	ClusterStateError     = "error"
)

const (
	DatabaseStateCreating  = "creating"
	DatabaseStateModifying = "modifying"
	DatabaseStateAvailable = "available"
	DatabaseStateDeleting  = "deleting"
	DatabaseStateDegraded  = "degraded"
	DatabaseStateUnknown   = "unknown"
	DatabaseStateFailed    = "failed"
)

const (
	HostStateHealthy     = "healthy"
	HostStateUnreachable = "unreachable"
	HostStateDegraded    = "degraded"
	HostStateUnknown     = "unknown"
)

const (
	InstanceStateCreating  = "creating"
	InstanceStateModifying = "modifying"
	InstanceStateBackingUp = "backing_up"
	InstanceStateAvailable = "available"
	InstanceStateDegraded  = "degraded"
	InstanceStateFailed    = "failed"
	InstanceStateUnknown   = "unknown"
)

const (
	PatroniStateStopping                     = "stopping"
	PatroniStateStopped                      = "stopped"
	PatroniStateStopFailed                   = "stop failed"
	PatroniStateCrashed                      = "crashed"
	PatroniStateRunning                      = "running"
	PatroniStateStarting                     = "starting"
	PatroniStateStartFailed                  = "start failed"
	PatroniStateRestarting                   = "restarting"
	PatroniStateRestartFailed                = "restart failed"
	PatroniStateInitializingNewCluster       = "initializing new cluster"
	PatroniStateInitDBFailed                 = "initdb failed"
	PatroniStateRunningCustomBootstrapScript = "running custom bootstrap script"
	PatroniStateCustomBootstrapFailed        = "custom bootstrap failed"
	PatroniStateCreatingReplica              = "creating replica"
	PatroniStateUnknown                      = "unknown"
)

const (
	RepositoryTypeS3    = "s3"
	RepositoryTypeGCS   = "gcs"
	RepositoryTypeAzure = "azure"
	RepositoryTypePosix = "posix"
	RepositoryTypeCIFS  = "cifs"
)

const (
	RetentionTypeTime  = "time"
	RetentionTypeCount = "count"
)

const (
	RoleReplica = "replica"
	RolePrimary = "primary"
)

const (
	TaskSortOrderAsc  = "asc"
	TaskSortOrderDesc = "desc"
)

const (
	TaskStatusPending   = "pending"
	TaskStatusRunning   = "running"
	TaskStatusCompleted = "completed"
	TaskStatusFailed    = "failed"
	TaskStatusUnknown   = "unknown"
	TaskStatusCanceling = "canceling"
	TaskStatusCanceled  = "canceled"
)
