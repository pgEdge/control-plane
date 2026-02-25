package swarm

import "github.com/pgEdge/control-plane/server/internal/resource"

func RegisterResourceTypes(registry *resource.Registry) {
	resource.RegisterResourceType[*PostgresServiceSpecResource](registry, ResourceTypePostgresServiceSpec)
	resource.RegisterResourceType[*PostgresService](registry, ResourceTypePostgresService)
	resource.RegisterResourceType[*ServiceInstanceSpecResource](registry, ResourceTypeServiceInstanceSpec)
	resource.RegisterResourceType[*ServiceInstanceResource](registry, ResourceTypeServiceInstance)
	resource.RegisterResourceType[*ServiceUserRole](registry, ResourceTypeServiceUserRole)
	resource.RegisterResourceType[*Network](registry, ResourceTypeNetwork)
	resource.RegisterResourceType[*PatroniConfig](registry, ResourceTypePatroniConfig)
	resource.RegisterResourceType[*PgBackRestRestore](registry, ResourceTypePgBackRestRestore)
	resource.RegisterResourceType[*CheckWillRestart](registry, ResourceTypeCheckWillRestart)
	resource.RegisterResourceType[*Switchover](registry, ResourceTypeSwitchover)
	resource.RegisterResourceType[*ScaleService](registry, ResourceTypeScaleService)
}
