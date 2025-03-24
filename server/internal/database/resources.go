package database

import "github.com/pgEdge/control-plane/server/internal/resource"

func RegisterResourceTypes(registry *resource.Registry) {
	resource.RegisterResourceType[*InstanceResource](registry, ResourceTypeInstance)
	resource.RegisterResourceType[*NodeResource](registry, ResourceTypeNode)
	resource.RegisterResourceType[*SubscriptionResource](registry, ResourceTypeSubscription)
}
