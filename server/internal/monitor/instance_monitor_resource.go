package monitor

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
)

var _ resource.Resource = (*InstanceMonitorResource)(nil)

const ResourceTypeInstanceMonitorResource resource.Type = "monitor.instance"

func InstanceMonitorResourceIdentifier(instanceID uuid.UUID) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID.String(),
		Type: ResourceTypeInstanceMonitorResource,
	}
}

type InstanceMonitorResource struct {
	DatabaseID   uuid.UUID `json:"database_id"`
	InstanceID   uuid.UUID `json:"instance_id"`
	DatabaseName string    `json:"db_name"`
	// InstanceHostname string    `json:"instance_hostname"`
	HostID uuid.UUID `json:"host_id"`
}

func (m *InstanceMonitorResource) ResourceVersion() string {
	return "1"
}

func (m *InstanceMonitorResource) DiffIgnore() []string {
	return nil
}

func (m *InstanceMonitorResource) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeHost,
		ID:   m.HostID.String(),
	}
}

func (m *InstanceMonitorResource) Identifier() resource.Identifier {
	return InstanceMonitorResourceIdentifier(m.InstanceID)
}

func (m *InstanceMonitorResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		database.InstanceResourceIdentifier(m.InstanceID),
	}
}

func (m *InstanceMonitorResource) Refresh(ctx context.Context, rc *resource.Context) error {
	service, err := do.Invoke[*Service](rc.Injector)
	if err != nil {
		return err
	}

	if !service.HasInstanceMonitor(m.InstanceID) {
		return resource.ErrNotFound
	}

	return nil
}

func (m *InstanceMonitorResource) Create(ctx context.Context, rc *resource.Context) error {
	service, err := do.Invoke[*Service](rc.Injector)
	if err != nil {
		return err
	}

	err = service.CreateInstanceMonitor(ctx, m.DatabaseID, m.InstanceID, m.DatabaseName)
	if err != nil {
		return fmt.Errorf("failed to create instance monitor: %w", err)
	}

	return nil
}

func (m *InstanceMonitorResource) Update(ctx context.Context, rc *resource.Context) error {
	return m.Create(ctx, rc)
}

func (m *InstanceMonitorResource) Delete(ctx context.Context, rc *resource.Context) error {
	service, err := do.Invoke[*Service](rc.Injector)
	if err != nil {
		return err
	}

	err = service.DeleteInstanceMonitor(ctx, m.InstanceID)
	if err != nil {
		return fmt.Errorf("failed to delete instance monitor: %w", err)
	}

	return nil
}
