package systemd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

const (
	serviceInstanceStartTimeout  = 5 * time.Minute
	serviceInstancePollInterval  = 2 * time.Second
	serviceInstanceStatusTimeout = 10 * time.Second
)

var _ resource.Resource = (*ServiceInstanceResource)(nil)

const ResourceTypeServiceInstance resource.Type = "systemd.service_instance"

func ServiceInstanceResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeServiceInstance,
	}
}

// ServiceInstanceResource tracks a service instance that runs as a systemd
// unit. It records the service instance in etcd and waits for the unit to
// start serving on create and update. The unit itself is managed by a
// UnitResource, which this resource depends on.
type ServiceInstanceResource struct {
	ServiceInstanceID string `json:"service_instance_id"`
	ServiceID         string `json:"service_id"`
	DatabaseID        string `json:"database_id"`
	HostID            string `json:"host_id"`
	UnitName          string `json:"unit_name"`
}

func (s *ServiceInstanceResource) ResourceVersion() string {
	return "1"
}

func (s *ServiceInstanceResource) DiffIgnore() []string {
	return nil
}

func (s *ServiceInstanceResource) Identifier() resource.Identifier {
	return ServiceInstanceResourceIdentifier(s.ServiceInstanceID)
}

func (s *ServiceInstanceResource) Executor() resource.Executor {
	return resource.HostExecutor(s.HostID)
}

func (s *ServiceInstanceResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		UnitResourceIdentifier(s.UnitName, s.DatabaseID, s.HostID),
	}
}

func (s *ServiceInstanceResource) TypeDependencies() []resource.Type {
	return nil
}

func (s *ServiceInstanceResource) Refresh(ctx context.Context, rc *resource.Context) error {
	_, err := readUnitFile(s.UnitName)
	if errors.Is(err, os.ErrNotExist) {
		return resource.ErrNotFound
	} else if err != nil {
		return err
	}

	// The service instance record is what makes the instance visible in the
	// API and to the monitor, so recreate it if it's missing.
	dbSvc, err := do.Invoke[*database.Service](rc.Injector)
	if err != nil {
		return err
	}
	_, err = dbSvc.GetServiceInstance(ctx, s.DatabaseID, s.ServiceInstanceID)
	if errors.Is(err, database.ErrServiceInstanceNotFound) {
		return resource.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("failed to get service instance: %w", err)
	}

	return nil
}

func (s *ServiceInstanceResource) Create(ctx context.Context, rc *resource.Context) error {
	dbSvc, err := do.Invoke[*database.Service](rc.Injector)
	if err != nil {
		return err
	}

	// Store the service instance with state="creating" so that it's visible
	// in the API even if it fails to start.
	err = dbSvc.UpdateServiceInstance(ctx, &database.ServiceInstanceUpdateOptions{
		ServiceInstanceID: s.ServiceInstanceID,
		ServiceID:         s.ServiceID,
		DatabaseID:        s.DatabaseID,
		HostID:            s.HostID,
		State:             database.ServiceInstanceStateCreating,
	})
	if err != nil {
		return fmt.Errorf("failed to store initial service instance: %w", err)
	}

	return s.waitForRunning(ctx, rc)
}

func (s *ServiceInstanceResource) Update(ctx context.Context, rc *resource.Context) error {
	// Refresh UpdatedAt before waiting so that the monitor's running-state
	// grace period covers a restart caused by this update. Best-effort: a
	// failure here only risks a transient false "failed" state.
	dbSvc, err := do.Invoke[*database.Service](rc.Injector)
	if err != nil {
		return err
	}
	err = dbSvc.SetServiceInstanceState(ctx, s.DatabaseID, s.ServiceInstanceID, database.ServiceInstanceStateRunning)
	if err != nil {
		if logger, logErr := do.Invoke[zerolog.Logger](rc.Injector); logErr == nil {
			logger.Warn().Err(err).
				Str("service_instance_id", s.ServiceInstanceID).
				Msg("failed to refresh service instance UpdatedAt before waiting for it to run")
		}
	}

	return s.waitForRunning(ctx, rc)
}

func (s *ServiceInstanceResource) Delete(ctx context.Context, rc *resource.Context) error {
	// The unit is stopped and removed by its UnitResource, which is deleted
	// after this resource because this resource depends on it.
	dbSvc, err := do.Invoke[*database.Service](rc.Injector)
	if err != nil {
		return err
	}
	if err := dbSvc.DeleteServiceInstance(ctx, s.DatabaseID, s.ServiceInstanceID); err != nil {
		return fmt.Errorf("failed to delete service instance: %w", err)
	}

	return nil
}

// waitForRunning waits until the service instance is serving requests and
// then records it as running.
func (s *ServiceInstanceResource) waitForRunning(ctx context.Context, rc *resource.Context) error {
	orch, err := do.Invoke[database.Orchestrator](rc.Injector)
	if err != nil {
		return err
	}
	dbSvc, err := do.Invoke[*database.Service](rc.Injector)
	if err != nil {
		return err
	}
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}
	logger = logger.With().
		Str("service_instance_id", s.ServiceInstanceID).
		Str("unit", s.UnitName).
		Logger()

	logger.Info().Msg("waiting for service instance to start")

	ctx, cancel := context.WithTimeout(ctx, serviceInstanceStartTimeout)
	defer cancel()

	for {
		ready, err := s.serving(ctx, orch)
		if err != nil {
			logger.Debug().Err(err).Msg("service instance is not serving yet")
		}
		if ready {
			break
		}
		if err := utils.SleepContext(ctx, serviceInstancePollInterval); err != nil {
			return fmt.Errorf("service instance did not start serving within %s: unit %s: check 'journalctl -u %s'",
				serviceInstanceStartTimeout, s.UnitName, s.UnitName)
		}
	}

	logger.Info().Msg("service instance is running")

	// Transition to running here rather than waiting for the monitor so that
	// the state is set deterministically by the time this resource is applied.
	err = dbSvc.SetServiceInstanceState(ctx, s.DatabaseID, s.ServiceInstanceID, database.ServiceInstanceStateRunning)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to update service instance state to running (monitor will handle it)")
	}

	return nil
}

func (s *ServiceInstanceResource) serving(ctx context.Context, orch database.Orchestrator) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, serviceInstanceStatusTimeout)
	defer cancel()

	status, err := orch.GetServiceInstanceStatus(ctx, s.ServiceInstanceID)
	if err != nil {
		return false, err
	}
	if status == nil || status.ServiceReady == nil {
		return false, nil
	}
	return *status.ServiceReady, nil
}
