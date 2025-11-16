package monitor

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

const statusMonitorInterval = 5 * time.Second

type InstanceMonitor struct {
	statusMonitor *Monitor
	databaseID    string
	instanceID    string
	dbName        string
	orch          database.Orchestrator
	dbSvc         *database.Service
	certSvc       *certificates.Service
}

func NewInstanceMonitor(
	orch database.Orchestrator,
	dbSvc *database.Service,
	certSvc *certificates.Service,
	logger zerolog.Logger,
	databaseID string,
	instanceID string,
	dbName string,
) *InstanceMonitor {
	m := &InstanceMonitor{
		databaseID: databaseID,
		instanceID: instanceID,
		dbName:     dbName,
		orch:       orch,
		dbSvc:      dbSvc,
		certSvc:    certSvc,
	}
	m.statusMonitor = NewMonitor(
		logger,
		statusMonitorInterval,
		m.checkStatus,
	)
	return m
}

func (m *InstanceMonitor) Start(ctx context.Context) {
	m.statusMonitor.Start(ctx)
}

func (m *InstanceMonitor) Stop() {
	m.statusMonitor.Stop()
}

func (m *InstanceMonitor) checkStatus(ctx context.Context) error {
	status := &database.InstanceStatus{
		StatusUpdatedAt: utils.PointerTo(time.Now()),
	}

	info, err := m.orch.GetInstanceConnectionInfo(ctx, m.databaseID, m.instanceID)
	if err != nil {
		if errors.Is(err, database.ErrInstanceStopped) {
			status.Stopped = utils.PointerTo(true)
			status.Error = utils.PointerTo(err.Error())
			return m.updateInstanceStatus(ctx, status)
		}
		return m.updateInstanceErrStatus(ctx, status, err)
	}
	status.Stopped = utils.PointerTo(false)
	status.Error = utils.PointerTo("")

	tlsCfg, err := m.certSvc.PostgresUserTLS(ctx, m.instanceID, info.InstanceHostname, "pgedge")
	if err != nil {
		return m.updateInstanceErrStatus(ctx, status, err)
	}

	status.Hostname = utils.PointerTo(info.ClientHost)
	status.IPv4Address = utils.PointerTo(info.ClientIPv4Address)
	status.Port = utils.PointerTo(info.ClientPort)

	err = m.populateFromPatroni(ctx, info, status)
	if err != nil {
		return m.updateInstanceErrStatus(ctx, status, err)
	}

	if status.IsPrimary() {
		err = m.populateFromDbConn(ctx, info, tlsCfg, status)
		if err != nil {
			return m.updateInstanceErrStatus(ctx, status, err)
		}
	}
	currentInstance, err := m.dbSvc.GetInstance(ctx, m.databaseID, m.instanceID)
	if err != nil {
		return m.updateInstanceErrStatus(ctx, status, err)
	}
	if currentInstance != nil && currentInstance.State != database.InstanceStateAvailable {

		_ = m.dbSvc.UpdateInstance(ctx, &database.InstanceUpdateOptions{
			InstanceID: m.instanceID,
			DatabaseID: m.databaseID,
			State:      database.InstanceStateAvailable,
			Error:      "",
		})
	}
	return m.updateInstanceStatus(ctx, status)
}

func (m *InstanceMonitor) populateFromPatroni(
	ctx context.Context,
	info *database.ConnectionInfo,
	status *database.InstanceStatus,
) error {
	client := patroni.NewClient(info.PatroniURL(), nil)
	patroniStatus, err := client.GetInstanceStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to get instance status: %w", err)
	}
	status.PatroniState = patroniStatus.State
	status.Role = patroniStatus.Role
	status.PatroniPaused = patroniStatus.Pause
	status.PendingRestart = patroniStatus.PendingRestart

	return nil
}

func (m *InstanceMonitor) populateFromDbConn(
	ctx context.Context,
	info *database.ConnectionInfo,
	tlsCfg *tls.Config,
	status *database.InstanceStatus,
) error {
	conn, err := database.ConnectToInstance(ctx, &database.ConnectionOptions{
		DSN: info.AdminDSN(m.dbName),
		TLS: tlsCfg,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to instance: %w", err)
	}
	defer conn.Close(ctx)

	pgVersion, err := postgres.GetPostgresVersion().Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to query postgres version: %w", err)
	}
	status.PostgresVersion = utils.PointerTo(pgVersion)

	spockVersion, err := postgres.GetSpockVersion().Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to query spock version: %w", err)
	}
	status.SpockVersion = utils.PointerTo(spockVersion)

	spockReadOnly, err := postgres.GetSpockReadOnly().Scalar(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to query spock read-only status: %w", err)
	}
	status.ReadOnly = utils.PointerTo(spockReadOnly)

	subStatuses, err := postgres.GetSubscriptionStatuses().Scalars(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to query subscription statuses: %w", err)
	}
	for _, sub := range subStatuses {
		status.Subscriptions = append(status.Subscriptions, database.SubscriptionStatus{
			ProviderNode: sub.ProviderNode,
			Name:         sub.SubscriptionName,
			Status:       sub.Status,
		})
	}

	return nil
}

func (m *InstanceMonitor) updateInstanceErrStatus(
	ctx context.Context,
	status *database.InstanceStatus,
	cause error,
) error {
	status.Error = utils.PointerTo(cause.Error())
	return m.updateInstanceStatus(ctx, status)
}

func (m *InstanceMonitor) updateInstanceStatus(ctx context.Context, status *database.InstanceStatus) error {
	err := m.dbSvc.UpdateInstanceStatus(ctx, m.databaseID, m.instanceID, status)
	if err != nil {
		return fmt.Errorf("failed to update instance status: %w", err)
	}

	return nil
}
