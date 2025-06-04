package monitor

import "github.com/pgEdge/control-plane/server/internal/resource"

func RegisterResourceTypes(registry *resource.Registry) {
	resource.RegisterResourceType[*InstanceMonitorResource](registry, ResourceTypeInstanceMonitorResource)
}
