package monitor

import (
	"context"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
)

var _ resource.Resource = (*ServiceInstanceMonitorResource)(nil)

const ResourceTypeServiceInstanceMonitorResource resource.Type = "monitor.service_instance"

func ServiceInstanceMonitorResourceIdentifier(serviceInstanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   serviceInstanceID,
		Type: ResourceTypeServiceInstanceMonitorResource,
	}
}

type ServiceInstanceMonitorResource struct {
	DatabaseID        string `json:"database_id"`
	ServiceInstanceID string `json:"service_instance_id"`
	HostID            string `json:"host_id"`
}

func (m *ServiceInstanceMonitorResource) ResourceVersion() string {
	return "1"
}

func (m *ServiceInstanceMonitorResource) DiffIgnore() []string {
	return nil
}

func (m *ServiceInstanceMonitorResource) Executor() resource.Executor {
	return resource.HostExecutor(m.HostID)
}

func (m *ServiceInstanceMonitorResource) Identifier() resource.Identifier {
	return ServiceInstanceMonitorResourceIdentifier(m.ServiceInstanceID)
}

func (m *ServiceInstanceMonitorResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		{
			ID:   m.ServiceInstanceID,
			Type: "swarm.service_instance",
		},
	}
}

func (m *ServiceInstanceMonitorResource) Refresh(ctx context.Context, rc *resource.Context) error {
	service, err := do.Invoke[*Service](rc.Injector)
	if err != nil {
		return err
	}

	if !service.HasServiceInstanceMonitor(m.ServiceInstanceID) {
		return resource.ErrNotFound
	}

	return nil
}

func (m *ServiceInstanceMonitorResource) Create(ctx context.Context, rc *resource.Context) error {
	service, err := do.Invoke[*Service](rc.Injector)
	if err != nil {
		return err
	}

	err = service.CreateServiceInstanceMonitor(ctx, m.DatabaseID, m.ServiceInstanceID, m.HostID)
	if err != nil {
		return fmt.Errorf("failed to create service instance monitor: %w", err)
	}

	return nil
}

func (m *ServiceInstanceMonitorResource) Update(ctx context.Context, rc *resource.Context) error {
	return m.Create(ctx, rc)
}

func (m *ServiceInstanceMonitorResource) Delete(ctx context.Context, rc *resource.Context) error {
	service, err := do.Invoke[*Service](rc.Injector)
	if err != nil {
		return err
	}

	err = service.DeleteServiceInstanceMonitor(ctx, m.ServiceInstanceID)
	if err != nil {
		return fmt.Errorf("failed to delete service instance monitor: %w", err)
	}

	return nil
}
