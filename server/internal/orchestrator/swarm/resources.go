package swarm

import "github.com/pgEdge/control-plane/server/internal/resource"

func RegisterResourceTypes(registry *resource.Registry) {
	resource.RegisterResourceType[*PostgresServiceSpecResource](registry, ResourceTypePostgresServiceSpec)
	resource.RegisterResourceType[*PostgresService](registry, ResourceTypePostgresService)
	resource.RegisterResourceType[*Network](registry, ResourceTypeNetwork)
	resource.RegisterResourceType[*EtcdCreds](registry, ResourceTypeEtcdCreds)
	resource.RegisterResourceType[*PatroniConfig](registry, ResourceTypePatroniConfig)
	resource.RegisterResourceType[*PostgresCerts](registry, ResourceTypePostgresCerts)
	resource.RegisterResourceType[*PgBackRestConfig](registry, ResourceTypePgBackRestConfig)
	resource.RegisterResourceType[*PgBackRestRestore](registry, ResourceTypePgBackRestRestore)
	resource.RegisterResourceType[*PgBackRestStanza](registry, ResourceTypePgBackRestStanza)
	resource.RegisterResourceType[*PatroniCluster](registry, ResourceTypePatroniCluster)
	resource.RegisterResourceType[*PatroniMember](registry, ResourceTypePatroniMember)
	resource.RegisterResourceType[*CheckWillRestart](registry, ResourceTypeCheckWillRestart)
	resource.RegisterResourceType[*Switchover](registry, ResourceTypeSwitchover)
}
