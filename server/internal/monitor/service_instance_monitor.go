package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

const serviceInstanceCreationTimeout = 5 * time.Minute

// serviceInstanceRunningGracePeriod is the time we tolerate nil or error status
// for a recently-transitioned "running" instance. This accounts for the delay
// between Docker Swarm service creation and container label propagation.
const serviceInstanceRunningGracePeriod = 30 * time.Second

type ServiceInstanceMonitor struct {
	statusMonitor     *Monitor
	dbOrch            database.Orchestrator
	dbSvc             *database.Service
	logger            zerolog.Logger
	databaseID        string
	serviceInstanceID string
	hostID            string
}

func NewServiceInstanceMonitor(
	orch database.Orchestrator,
	dbSvc *database.Service,
	logger zerolog.Logger,
	databaseID string,
	serviceInstanceID string,
	hostID string,
) *ServiceInstanceMonitor {
	m := &ServiceInstanceMonitor{
		dbOrch:            orch,
		dbSvc:             dbSvc,
		logger:            logger,
		databaseID:        databaseID,
		serviceInstanceID: serviceInstanceID,
		hostID:            hostID,
	}
	m.statusMonitor = NewMonitor(
		logger,
		database.ServiceInstanceMonitorRefreshInterval,
		m.checkStatus,
	)
	return m
}

func (m *ServiceInstanceMonitor) Start(ctx context.Context) {
	m.statusMonitor.Start(ctx)
}

func (m *ServiceInstanceMonitor) Stop() {
	m.statusMonitor.Stop()
}

func (m *ServiceInstanceMonitor) checkStatus(ctx context.Context) error {
	m.logger.Debug().
		Str("service_instance_id", m.serviceInstanceID).
		Str("database_id", m.databaseID).
		Msg("checking service instance status")

	// Get current service instance
	serviceInstance, err := m.dbSvc.GetServiceInstance(ctx, m.databaseID, m.serviceInstanceID)
	if err != nil {
		m.logger.Err(err).
			Str("service_instance_id", m.serviceInstanceID).
			Str("database_id", m.databaseID).
			Msg("failed to get service instance")
		return fmt.Errorf("failed to get service instance: %w", err)
	}

	if serviceInstance == nil {
		m.logger.Warn().
			Str("service_instance_id", m.serviceInstanceID).
			Str("database_id", m.databaseID).
			Msg("service instance not found")
		return fmt.Errorf("service instance not found")
	}

	// Get status from orchestrator
	status, err := m.dbOrch.GetServiceInstanceStatus(ctx, m.serviceInstanceID)
	if err != nil {
		m.logger.Err(err).
			Str("service_instance_id", m.serviceInstanceID).
			Str("database_id", m.databaseID).
			Msg("failed to get service instance status from orchestrator")
		return m.handleStatusError(ctx, serviceInstance, err)
	}

	// Handle state transitions based on current state and status
	return m.handleStateTransition(ctx, serviceInstance, status)
}

func (m *ServiceInstanceMonitor) handleStatusError(
	ctx context.Context,
	serviceInstance *database.ServiceInstance,
	statusErr error,
) error {
	// If we're in creating state and timeout has elapsed, mark as failed
	if serviceInstance.State == database.ServiceInstanceStateCreating {
		elapsed := time.Since(serviceInstance.CreatedAt)
		if elapsed > serviceInstanceCreationTimeout {
			m.logger.Error().
				Str("service_instance_id", m.serviceInstanceID).
				Str("database_id", m.databaseID).
				Dur("elapsed", elapsed).
				Msg("service instance creation timeout - marking as failed")

			return m.updateState(ctx, database.ServiceInstanceStateFailed, nil,
				fmt.Sprintf("creation timeout after %s: %v", elapsed, statusErr))
		}
		// Still within timeout, don't update state yet
		return statusErr
	}

	// If we're in running state and getting errors, allow a grace period
	// for recently-transitioned instances (container labels may not be visible yet)
	if serviceInstance.State == database.ServiceInstanceStateRunning {
		if time.Since(serviceInstance.UpdatedAt) < serviceInstanceRunningGracePeriod {
			m.logger.Warn().
				Str("service_instance_id", m.serviceInstanceID).
				Str("database_id", m.databaseID).
				Err(statusErr).
				Msg("service instance status check failed but within grace period")
			return statusErr
		}

		m.logger.Error().
			Str("service_instance_id", m.serviceInstanceID).
			Str("database_id", m.databaseID).
			Err(statusErr).
			Msg("service instance status check failed - marking as failed")

		return m.updateState(ctx, database.ServiceInstanceStateFailed, nil,
			fmt.Sprintf("status check failed: %v", statusErr))
	}

	return statusErr
}

func (m *ServiceInstanceMonitor) handleStateTransition(
	ctx context.Context,
	serviceInstance *database.ServiceInstance,
	status *database.ServiceInstanceStatus,
) error {
	// Status is nil means container is not running
	if status == nil {
		return m.handleNilStatus(ctx, serviceInstance)
	}

	// Check container health
	isHealthy := m.isContainerHealthy(status)

	switch serviceInstance.State {
	case database.ServiceInstanceStateCreating:
		// Transition from creating to running when container is healthy
		if isHealthy {
			m.logger.Info().
				Str("service_instance_id", m.serviceInstanceID).
				Str("database_id", m.databaseID).
				Msg("service instance is healthy - transitioning to running")
			return m.updateState(ctx, database.ServiceInstanceStateRunning, status, "")
		}

		// Still creating, check for timeout
		elapsed := time.Since(serviceInstance.CreatedAt)
		if elapsed > serviceInstanceCreationTimeout {
			m.logger.Error().
				Str("service_instance_id", m.serviceInstanceID).
				Str("database_id", m.databaseID).
				Dur("elapsed", elapsed).
				Msg("service instance creation timeout - container not healthy")
			return m.updateState(ctx, database.ServiceInstanceStateFailed, status,
				fmt.Sprintf("creation timeout after %s - container not healthy", elapsed))
		}

		// Update status but keep state as creating
		return m.updateStatus(ctx, status)

	case database.ServiceInstanceStateRunning:
		// If no longer healthy, mark as failed
		if !isHealthy {
			m.logger.Error().
				Str("service_instance_id", m.serviceInstanceID).
				Str("database_id", m.databaseID).
				Msg("service instance is no longer healthy - marking as failed")
			return m.updateState(ctx, database.ServiceInstanceStateFailed, status,
				"container is no longer healthy")
		}

		// Update status to keep it fresh
		return m.updateStatus(ctx, status)

	default:
		// For other states (failed, deleting), just update status
		return m.updateStatus(ctx, status)
	}
}

func (m *ServiceInstanceMonitor) handleNilStatus(
	ctx context.Context,
	serviceInstance *database.ServiceInstance,
) error {
	switch serviceInstance.State {
	case database.ServiceInstanceStateCreating:
		// Check for timeout
		elapsed := time.Since(serviceInstance.CreatedAt)
		if elapsed > serviceInstanceCreationTimeout {
			m.logger.Error().
				Str("service_instance_id", m.serviceInstanceID).
				Str("database_id", m.databaseID).
				Dur("elapsed", elapsed).
				Msg("service instance creation timeout - no status available")
			return m.updateState(ctx, database.ServiceInstanceStateFailed, nil,
				fmt.Sprintf("creation timeout after %s - no status available", elapsed))
		}
		// Still within timeout, but log for debugging
		m.logger.Debug().
			Str("service_instance_id", m.serviceInstanceID).
			Str("database_id", m.databaseID).
			Dur("elapsed", elapsed).
			Msg("service instance status not yet available (still waiting)")
		return nil

	case database.ServiceInstanceStateRunning:
		// Allow a grace period for recently-transitioned instances
		// (container labels may not be visible yet after deployment)
		if time.Since(serviceInstance.UpdatedAt) < serviceInstanceRunningGracePeriod {
			m.logger.Warn().
				Str("service_instance_id", m.serviceInstanceID).
				Str("database_id", m.databaseID).
				Msg("service instance status not available but within grace period")
			return nil
		}

		// Container disappeared, mark as failed
		m.logger.Error().
			Str("service_instance_id", m.serviceInstanceID).
			Str("database_id", m.databaseID).
			Msg("service instance status not available - marking as failed")
		return m.updateState(ctx, database.ServiceInstanceStateFailed, nil,
			"container status not available")

	default:
		// For other states, nil status is acceptable
		return nil
	}
}

func (m *ServiceInstanceMonitor) isContainerHealthy(status *database.ServiceInstanceStatus) bool {
	// Service is ready if ServiceReady flag is true
	if status.ServiceReady != nil && *status.ServiceReady {
		return true
	}

	// Also check health check status if available
	if status.HealthCheck != nil {
		return status.HealthCheck.Status == "healthy"
	}

	// If no explicit health information, consider it healthy if status exists
	// (container is running)
	return true
}

func (m *ServiceInstanceMonitor) updateState(
	ctx context.Context,
	state database.ServiceInstanceState,
	status *database.ServiceInstanceStatus,
	errorMsg string,
) error {
	m.logger.Debug().
		Str("service_instance_id", m.serviceInstanceID).
		Str("database_id", m.databaseID).
		Str("new_state", string(state)).
		Str("error", errorMsg).
		Msg("updating service instance state")

	err := m.dbSvc.UpdateServiceInstanceState(ctx, m.serviceInstanceID, &database.ServiceInstanceStateUpdate{
		DatabaseID: m.databaseID,
		State:      state,
		Status:     status,
		Error:      errorMsg,
	})
	if err != nil {
		return fmt.Errorf("failed to update service instance state: %w", err)
	}

	return nil
}

func (m *ServiceInstanceMonitor) updateStatus(
	ctx context.Context,
	status *database.ServiceInstanceStatus,
) error {
	// Update last health check time
	now := time.Now()
	status.LastHealthAt = utils.PointerTo(now)

	m.logger.Debug().
		Str("service_instance_id", m.serviceInstanceID).
		Str("database_id", m.databaseID).
		Msg("updating service instance status")

	err := m.dbSvc.UpdateServiceInstanceStatus(ctx, m.databaseID, m.serviceInstanceID, status)
	if err != nil {
		return fmt.Errorf("failed to update service instance status: %w", err)
	}

	return nil
}
