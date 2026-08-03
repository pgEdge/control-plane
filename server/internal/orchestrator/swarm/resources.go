package swarm

import "github.com/pgEdge/control-plane/server/internal/resource"

func RegisterResourceTypes(registry *resource.Registry) {
	resource.RegisterResourceType[*PostgresServiceSpecResource](registry, ResourceTypePostgresServiceSpec)
	resource.RegisterResourceType[*PostgresService](registry, ResourceTypePostgresService)
	resource.RegisterResourceType[*ServiceInstanceSpecResource](registry, ResourceTypeServiceInstanceSpec)
	resource.RegisterResourceType[*ServiceInstanceResource](registry, ResourceTypeServiceInstance)
	resource.RegisterResourceType[*Network](registry, ResourceTypeNetwork)
	resource.RegisterResourceType[*PatroniConfig](registry, ResourceTypePatroniConfig)
	resource.RegisterResourceType[*PgBackRestRestore](registry, ResourceTypePgBackRestRestore)
	resource.RegisterResourceType[*CheckWillRestart](registry, ResourceTypeCheckWillRestart)
	resource.RegisterResourceType[*Switchover](registry, ResourceTypeSwitchover)
	resource.RegisterResourceType[*ScaleService](registry, ResourceTypeScaleService)
	resource.RegisterResourceType[*MCPConfigResource](registry, ResourceTypeMCPConfig)
	resource.RegisterResourceType[*PostgRESTPreflightResource](registry, ResourceTypePostgRESTPreflightResource)
	resource.RegisterResourceType[*PostgRESTConfigResource](registry, ResourceTypePostgRESTConfig)
	resource.RegisterResourceType[*PostgRESTAuthenticatorResource](registry, ResourceTypePostgRESTAuthenticator)
	resource.RegisterResourceType[*RAGPreflightResource](registry, ResourceTypeRAGPreflightResource)
	resource.RegisterResourceType[*RAGServiceKeysResource](registry, ResourceTypeRAGServiceKeys)
	resource.RegisterResourceType[*RAGConfigResource](registry, ResourceTypeRAGConfig)
	resource.RegisterResourceType[*LakekeeperConfigResource](registry, ResourceTypeLakekeeperConfig)
	resource.RegisterResourceType[*LakekeeperBootstrapResource](registry, ResourceTypeLakekeeperBootstrap)
	resource.RegisterResourceType[*LakekeeperStorageSecretResource](registry, ResourceTypeLakekeeperStorageSecret)
	resource.RegisterResourceType[*LakekeeperCatalogDBResource](registry, ResourceTypeLakekeeperCatalogDB)
	resource.RegisterResourceType[*LakekeeperColdfrontExtensionResource](registry, ResourceTypeLakekeeperColdfrontExtension)
}
