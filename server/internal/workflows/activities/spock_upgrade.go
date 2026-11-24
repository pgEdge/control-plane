package activities

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

// These activities support a Spock major version upgrade. Each one runs on
// the host of the instance it connects to, because the client certificate is
// only valid there.

type SetSpockDDLReplicationInput struct {
	DatabaseID string `json:"database_id"`
	InstanceID string `json:"instance_id"`
	Paused     bool   `json:"paused"`
}

type SetSpockDDLReplicationOutput struct{}

func (a *Activities) ExecuteSetSpockDDLReplication(
	ctx workflow.Context,
	hostID string,
	input *SetSpockDDLReplicationInput,
) workflow.Future[*SetSpockDDLReplicationOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 3,
		},
	}
	return workflow.ExecuteActivity[*SetSpockDDLReplicationOutput](ctx, options, a.SetSpockDDLReplication, input)
}

// SetSpockDDLReplication pauses or resumes Spock's automatic DDL replication
// on one instance. It works on primaries and standbys, and the setting
// persists across restarts and switchovers.
func (a *Activities) SetSpockDDLReplication(ctx context.Context, input *SetSpockDDLReplicationInput) (*SetSpockDDLReplicationOutput, error) {
	if input == nil {
		return nil, errors.New("input is nil")
	}
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID, "instance_id", input.InstanceID)
	logger.Info("setting spock ddl replication", "paused", input.Paused)

	conn, _, err := a.connectToInstance(ctx, input.DatabaseID, input.InstanceID, "postgres")
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	stmt := postgres.ResumeDDLReplication()
	if input.Paused {
		stmt = postgres.PauseDDLReplication()
	}
	if err := stmt.Exec(ctx, conn); err != nil {
		return nil, fmt.Errorf("failed to set spock ddl replication on instance %s: %w", input.InstanceID, err)
	}

	return &SetSpockDDLReplicationOutput{}, nil
}

type GetSpockDDLReplicationPausedInput struct {
	DatabaseID string `json:"database_id"`
	InstanceID string `json:"instance_id"`
}

type GetSpockDDLReplicationPausedOutput struct {
	Paused bool `json:"paused"`
}

func (a *Activities) ExecuteGetSpockDDLReplicationPaused(
	ctx workflow.Context,
	hostID string,
	input *GetSpockDDLReplicationPausedInput,
) workflow.Future[*GetSpockDDLReplicationPausedOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*GetSpockDDLReplicationPausedOutput](ctx, options, a.GetSpockDDLReplicationPaused, input)
}

// GetSpockDDLReplicationPaused reports whether a Spock major upgrade left DDL
// replication paused on one instance.
func (a *Activities) GetSpockDDLReplicationPaused(ctx context.Context, input *GetSpockDDLReplicationPausedInput) (*GetSpockDDLReplicationPausedOutput, error) {
	if input == nil {
		return nil, errors.New("input is nil")
	}

	conn, _, err := a.connectToInstance(ctx, input.DatabaseID, input.InstanceID, "postgres")
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	paused, err := postgres.DDLReplicationPaused().Scalar(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to check spock ddl replication on instance %s: %w", input.InstanceID, err)
	}

	return &GetSpockDDLReplicationPausedOutput{Paused: paused}, nil
}

type EnableSpockSlotsFailoverInput struct {
	DatabaseID   string `json:"database_id"`
	InstanceID   string `json:"instance_id"`
	DatabaseName string `json:"database_name"`
}

type EnableSpockSlotsFailoverOutput struct{}

func (a *Activities) ExecuteEnableSpockSlotsFailover(
	ctx workflow.Context,
	hostID string,
	input *EnableSpockSlotsFailoverInput,
) workflow.Future[*EnableSpockSlotsFailoverOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 3,
		},
	}
	return workflow.ExecuteActivity[*EnableSpockSlotsFailoverOutput](ctx, options, a.EnableSpockSlotsFailover, input)
}

// EnableSpockSlotsFailover sets the failover flag on the Spock slots of a
// primary on Postgres 17 or later, before a Spock major upgrade starts.
// Spock 6 replicas on Postgres 17+ get their slot copies from native slot
// sync, which only copies flagged slots, and slots created under Spock 5
// aren't flagged. Without this, a Spock 6 replica promoted during the rolling
// upgrade would have no copies of the slots its node's subscribers use. It
// does nothing on Postgres 16.
func (a *Activities) EnableSpockSlotsFailover(ctx context.Context, input *EnableSpockSlotsFailoverInput) (*EnableSpockSlotsFailoverOutput, error) {
	if input == nil {
		return nil, errors.New("input is nil")
	}
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID, "instance_id", input.InstanceID)

	conn, opts, err := a.connectToInstance(ctx, input.DatabaseID, input.InstanceID, input.DatabaseName)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	if err := enableSlotsFailover(ctx, conn, opts); err != nil {
		return nil, fmt.Errorf("instance %s: %w", input.InstanceID, err)
	}
	logger.Info("spock replication slots have failover enabled")

	return &EnableSpockSlotsFailoverOutput{}, nil
}

type VerifySpockUpgradeInput struct {
	DatabaseID   string `json:"database_id"`
	InstanceID   string `json:"instance_id"`
	DatabaseName string `json:"database_name"`
}

type VerifySpockUpgradeOutput struct{}

func (a *Activities) ExecuteVerifySpockUpgrade(
	ctx workflow.Context,
	hostID string,
	input *VerifySpockUpgradeInput,
) workflow.Future[*VerifySpockUpgradeOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*VerifySpockUpgradeOutput](ctx, options, a.VerifySpockUpgrade, input)
}

// VerifySpockUpgrade checks a node's primary after a Spock major upgrade: the
// Spock extension matches the new binaries, every Spock slot has the failover
// flag (Postgres 17+), and the node's subscriptions are replicating.
func (a *Activities) VerifySpockUpgrade(ctx context.Context, input *VerifySpockUpgradeInput) (*VerifySpockUpgradeOutput, error) {
	const (
		extensionUpdateTimeout = 2 * time.Minute
		replicatingTimeout     = 5 * time.Minute
	)

	if input == nil {
		return nil, errors.New("input is nil")
	}
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID, "instance_id", input.InstanceID)

	conn, opts, err := a.connectToInstance(ctx, input.DatabaseID, input.InstanceID, input.DatabaseName)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	if err := database.EnsureSpockExtensionUpdated(ctx, conn, extensionUpdateTimeout); err != nil {
		return nil, fmt.Errorf("instance %s: %w", input.InstanceID, err)
	}
	// Spock's extension update flags the existing slots itself, but skips the
	// ones that are in use.
	if err := enableSlotsFailover(ctx, conn, opts); err != nil {
		return nil, fmt.Errorf("instance %s: %w", input.InstanceID, err)
	}
	if err := database.WaitForSubscriptionsReplicating(ctx, conn, replicatingTimeout); err != nil {
		return nil, fmt.Errorf("instance %s: %w", input.InstanceID, err)
	}
	logger.Info("verified spock upgrade")

	return &VerifySpockUpgradeOutput{}, nil
}

func enableSlotsFailover(ctx context.Context, conn *pgx.Conn, opts *database.ConnectionOptions) error {
	supported, err := database.SupportsSlotFailover(ctx, conn)
	if err != nil || !supported {
		return err
	}
	repl, err := database.ConnectReplicationToInstance(ctx, opts)
	if err != nil {
		return err
	}
	defer func(repl *pgconn.PgConn) {
		_ = repl.Close(context.Background())
	}(repl)

	return database.EnableSpockSlotsFailover(ctx, conn, repl)
}

// connectToInstance connects to the given database on an instance as the
// pgedge user. It also returns the connection options, for opening another
// connection to the same instance.
func (a *Activities) connectToInstance(
	ctx context.Context,
	databaseID, instanceID, dbName string,
) (*pgx.Conn, *database.ConnectionOptions, error) {
	certSvc, err := do.Invoke[*certificates.Service](a.Injector)
	if err != nil {
		return nil, nil, err
	}
	info, err := a.DatabaseService.GetInstanceConnectionInfo(ctx, databaseID, instanceID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get instance connection info: %w", err)
	}
	tlsCfg, err := certSvc.PostgresUserTLS(ctx, instanceID, info.InstanceHostname, "pgedge")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get TLS config: %w", err)
	}
	opts := &database.ConnectionOptions{
		DSN: info.AdminDSN(dbName),
		TLS: tlsCfg,
	}
	conn, err := database.ConnectToInstance(ctx, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to instance %s: %w", instanceID, err)
	}
	return conn, opts, nil
}
