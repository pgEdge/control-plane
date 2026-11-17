package common

import "github.com/pgEdge/control-plane/server/internal/resource"

func RegisterResourceTypes(registry *resource.Registry) {
	resource.RegisterResourceType[*EtcdCreds](registry, ResourceTypeEtcdCreds)
	resource.RegisterResourceType[*PostgresCerts](registry, ResourceTypePostgresCerts)
	resource.RegisterResourceType[*PgBackRestConfig](registry, ResourceTypePgBackRestConfig)
	resource.RegisterResourceType[*PgBackRestStanza](registry, ResourceTypePgBackRestStanza)
	resource.RegisterResourceType[*PatroniCluster](registry, ResourceTypePatroniCluster)
	resource.RegisterResourceType[*PatroniMember](registry, ResourceTypePatroniMember)
	resource.RegisterResourceType[*PgServiceConf](registry, ResourceTypePgServiceConf)
	resource.RegisterResourceType[*PostgRESTPreflightResource](registry, ResourceTypePostgRESTPreflightResource)
	resource.RegisterResourceType[*PostgRESTConfigResource](registry, ResourceTypePostgRESTConfig)
	resource.RegisterResourceType[*PostgRESTAuthenticatorResource](registry, ResourceTypePostgRESTAuthenticator)
	resource.RegisterResourceType[*MCPConfigResource](registry, ResourceTypeMCPConfig)
}
