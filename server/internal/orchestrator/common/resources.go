package common

import "github.com/pgEdge/control-plane/server/internal/resource"

func RegisterResourceTypes(registry *resource.Registry) {
	resource.RegisterResourceType[*EtcdCreds](registry, ResourceTypeEtcdCreds)
	resource.RegisterResourceType[*PostgresCerts](registry, ResourceTypePostgresCerts)
	resource.RegisterResourceType[*PgBackRestConfig](registry, ResourceTypePgBackRestConfig)
	resource.RegisterResourceType[*PgBackRestStanza](registry, ResourceTypePgBackRestStanza)
	resource.RegisterResourceType[*PatroniCluster](registry, ResourceTypePatroniCluster)
	resource.RegisterResourceType[*PatroniMember](registry, ResourceTypePatroniMember)
}
