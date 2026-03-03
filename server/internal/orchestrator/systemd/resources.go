package systemd

import "github.com/pgEdge/control-plane/server/internal/resource"

func RegisterResourceTypes(registry *resource.Registry) {
	resource.RegisterResourceType[*PatroniConfig](registry, ResourceTypePatroniConfig)
	resource.RegisterResourceType[*UnitResource](registry, ResourceTypeUnit)
	resource.RegisterResourceType[*PgBackRestRestore](registry, ResourceTypePgBackRestRestore)
}
