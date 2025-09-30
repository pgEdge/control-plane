//go:build e2e_test

package e2e

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/client"
)

type DatabaseFixture struct {
	*controlplane.Database

	config TestConfig
	client *client.MultiServerClient
}

func NewDatabaseFixture(
	ctx context.Context,
	config TestConfig,
	client *client.MultiServerClient,
	req *controlplane.CreateDatabaseRequest,
) (*DatabaseFixture, error) {
	db := &DatabaseFixture{
		config: config,
		client: client,
	}
	if err := db.create(ctx, req); err != nil {
		return nil, err
	}
	return db, nil
}

func (d *DatabaseFixture) create(ctx context.Context, req *controlplane.CreateDatabaseRequest) error {
	resp, err := d.client.CreateDatabase(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}

	d.Database = resp.Database

	if err := d.waitForTask(ctx, resp.Task); err != nil {
		return err
	}

	return d.Refresh(ctx)
}

type UpdateOptions struct {
	ForceUpdate bool
	TenantID    *controlplane.Identifier
	Spec        *controlplane.DatabaseSpec
}

func (d *DatabaseFixture) Update(ctx context.Context, options UpdateOptions) error {
	resp, err := d.client.UpdateDatabase(ctx, &controlplane.UpdateDatabasePayload{
		DatabaseID:  d.ID,
		ForceUpdate: options.ForceUpdate,
		Request: &controlplane.UpdateDatabaseRequest{
			TenantID: options.TenantID,
			Spec:     options.Spec,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to update database: %w", err)
	}

	if err := d.waitForTask(ctx, resp.Task); err != nil {
		return err
	}

	return d.Refresh(ctx)
}

func (d *DatabaseFixture) Delete(ctx context.Context, force bool) error {
	resp, err := d.client.DeleteDatabase(ctx, &controlplane.DeleteDatabasePayload{
		DatabaseID: d.ID,
		Force:      force,
	})
	if err != nil {
		return fmt.Errorf("failed to delete database: %w", err)
	}

	if err := d.waitForTask(ctx, resp.Task); err != nil {
		return err
	}

	return nil
}

func (d *DatabaseFixture) EnsureDelete(ctx context.Context) error {
	resp, err := d.client.DeleteDatabase(ctx, &controlplane.DeleteDatabasePayload{
		DatabaseID: d.ID,
		Force:      true,
	})
	switch {
	case errors.Is(err, client.ErrNotFound):
		return nil
	case err != nil:
		return fmt.Errorf("failed to delete database: %w", err)
	}

	if err := d.waitForTask(ctx, resp.Task); err != nil {
		return err
	}

	return nil
}

type BackupDatabaseNodeOptions struct {
	Node    string
	Options *controlplane.BackupOptions
	Force   bool
}

func (d *DatabaseFixture) BackupDatabaseNode(ctx context.Context, options BackupDatabaseNodeOptions) error {
	resp, err := d.client.BackupDatabaseNode(ctx, &controlplane.BackupDatabaseNodePayload{
		DatabaseID: d.ID,
		NodeName:   options.Node,
		Options:    options.Options,
		Force:      options.Force,
	})
	if err != nil {
		return fmt.Errorf("failed to get backup database node: %w", err)
	}

	if err := d.waitForTask(ctx, resp.Task); err != nil {
		return err
	}

	return nil
}

type RestoreDatabaseOptions struct {
	TargetNodes   []string
	RestoreConfig *controlplane.RestoreConfigSpec
	Force         bool
}

func (d *DatabaseFixture) RestoreDatabase(ctx context.Context, options RestoreDatabaseOptions) error {
	resp, err := d.client.RestoreDatabase(ctx, &controlplane.RestoreDatabasePayload{
		Force:      options.Force,
		DatabaseID: d.ID,
		Request: &controlplane.RestoreDatabaseRequest{
			RestoreConfig: options.RestoreConfig,
			TargetNodes:   options.TargetNodes,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to restore database: %w", err)
	}

	if err := d.waitForTask(ctx, resp.Task); err != nil {
		return err
	}

	return d.Refresh(ctx)
}

type RestartInstanceOptions struct {
	InstanceID     string
	RestartOptions *controlplane.RestartOptions
}

func (d *DatabaseFixture) RestartInstance(ctx context.Context, options RestartInstanceOptions) error {
	resp, err := d.client.RestartInstance(ctx, &controlplane.RestartInstancePayload{
		DatabaseID:     d.ID,
		InstanceID:     controlplane.Identifier(options.InstanceID),
		RestartOptions: options.RestartOptions,
	})
	if err != nil {
		return fmt.Errorf("failed to restart instance: %w", err)
	}

	if err := d.waitForTask(ctx, resp); err != nil {
		return err
	}

	return d.Refresh(ctx)
}

func (d *DatabaseFixture) Refresh(ctx context.Context) error {
	db, err := d.client.GetDatabase(ctx, &controlplane.GetDatabasePayload{
		DatabaseID: d.ID,
	})
	if err != nil {
		return fmt.Errorf("failed to get updated database: %w", err)
	}

	d.Database = db

	return nil
}

type ConnectionOptions struct {
	Matcher    InstanceMatcher
	InstanceID string
	Username   string
	Password   string
}

func (d *DatabaseFixture) ConnectToInstance(ctx context.Context, opts ConnectionOptions) (*pgx.Conn, error) {
	var instance *controlplane.Instance
	switch {
	case opts.InstanceID != "":
		instance = d.GetInstance(WithID(opts.InstanceID))
	case opts.Matcher != nil:
		instance = d.GetInstance(opts.Matcher)
	default:
		instance = d.Instances[0]
	}
	if instance == nil {
		return nil, errors.New("no matching instance found")
	}
	if instance.ConnectionInfo == nil {
		return nil, fmt.Errorf("instance %s has no connection info", instance.ID)
	}
	if instance.ConnectionInfo.Port == nil {
		return nil, fmt.Errorf("instance %s connection info is missing port has no connection info", instance.ID)
	}

	host, ok := d.config.Hosts[instance.HostID]
	if !ok {
		return nil, fmt.Errorf("host %s not found in fixture's hosts", instance.HostID)
	}

	dsn := fmt.Sprintf("host=%s port=%d dbname=%s user=%s", host.ExternalIP, *instance.ConnectionInfo.Port, d.Spec.DatabaseName, opts.Username)
	if opts.Password != "" {
		dsn = fmt.Sprintf("%s password=%s", dsn, opts.Password)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to instance: %w", err)
	}

	return conn, nil
}

func (d *DatabaseFixture) WithConnection(ctx context.Context, opts ConnectionOptions, t testing.TB, do func(conn *pgx.Conn)) {
	conn, err := d.ConnectToInstance(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}

	do(conn)

	if err := conn.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

type InstanceMatcher func(inst *controlplane.Instance) bool

func WithID(instanceID string) InstanceMatcher {
	return func(inst *controlplane.Instance) bool {
		return inst.ID == instanceID
	}
}

func WithRole(role string) InstanceMatcher {
	return func(inst *controlplane.Instance) bool {
		return inst.Postgres != nil &&
			inst.Postgres.Role != nil &&
			*inst.Postgres.Role == role
	}
}

func WithHost(hostID string) InstanceMatcher {
	return func(inst *controlplane.Instance) bool {
		return inst.HostID == hostID
	}
}

func WithNode(node string) InstanceMatcher {
	return func(inst *controlplane.Instance) bool {
		return inst.NodeName == node
	}
}

func And(matchers ...InstanceMatcher) InstanceMatcher {
	return func(inst *controlplane.Instance) bool {
		for _, m := range matchers {
			if !m(inst) {
				return false
			}
		}
		return true
	}
}

func Or(matchers ...InstanceMatcher) InstanceMatcher {
	return func(inst *controlplane.Instance) bool {
		for _, m := range matchers {
			if m(inst) {
				return true
			}
		}
		return false
	}
}

func (d *DatabaseFixture) GetInstance(matcher InstanceMatcher) *controlplane.Instance {
	for _, inst := range d.Instances {
		if matcher(inst) {
			return inst
		}
	}
	return nil
}

func (d *DatabaseFixture) waitForTask(ctx context.Context, task *controlplane.Task) error {
	task, err := d.client.WaitForTask(ctx, &controlplane.GetDatabaseTaskPayload{
		DatabaseID: d.ID,
		TaskID:     task.TaskID,
	})
	if err != nil {
		return fmt.Errorf("failed to wait for task: %w", err)
	}
	if task.Status != client.TaskStatusCompleted {
		var taskError string
		if task.Error != nil {
			taskError = *task.Error
		}
		return fmt.Errorf("task status is '%s' instead of 'completed', error=%s", task.Status, taskError)
	}

	return nil
}

func (f *DatabaseFixture) VerifySpockReplication(ctx context.Context, t testing.TB, nodes []*controlplane.DatabaseNodeSpec, opts ConnectionOptions) {

	t.Log("Verifying spock nodes are in sync")

	// Execute sync_event on all primary nodes
	nodeSyncMap := make(map[string]string)
	for _, node := range nodes {
		primaryOpts := ConnectionOptions{
			Matcher:  And(WithNode(node.Name), WithRole("primary")),
			Username: opts.Username,
			Password: opts.Password,
		}

		f.WithConnection(ctx, primaryOpts, t, func(conn *pgx.Conn) {
			var syncLSN string

			row := conn.QueryRow(ctx, "SELECT spock.sync_event();")
			require.NoError(t, row.Scan(&syncLSN))

			assert.NotEmpty(t, syncLSN)

			nodeSyncMap[node.Name] = syncLSN
		})
	}

	// Verify wait_for_sync_event on all other nodes
	for _, node := range nodes {

		primaryOpts := ConnectionOptions{
			Matcher:  And(WithNode(node.Name), WithRole("primary")),
			Username: opts.Username,
			Password: opts.Password,
		}

		for _, peerNode := range nodes {
			if peerNode.Name == node.Name {
				continue
			}

			f.WithConnection(ctx, primaryOpts, t, func(conn *pgx.Conn) {
				var synced bool

				row := conn.QueryRow(ctx, "CALL spock.wait_for_sync_event(true, $1, $2::pg_lsn, 30);", peerNode.Name, nodeSyncMap[peerNode.Name])
				require.NoError(t, row.Scan(&synced))
				assert.True(t, synced)
			})
		}
	}
}

func (d *DatabaseFixture) SwitchoverDatabaseNode(ctx context.Context, req *controlplane.SwitchoverDatabaseNodePayload) error {

	resp, err := d.client.SwitchoverDatabaseNode(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to call switchover api: %w", err)
	}

	if err := d.waitForTask(ctx, resp.Task); err != nil {
		return err
	}

	// refresh local db state
	return d.Refresh(ctx)
}
