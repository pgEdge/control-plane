package database

import "github.com/pgEdge/control-plane/server/internal/resource"

func RegisterResourceTypes(registry *resource.Registry) {
	resource.RegisterResourceType[*InstanceResource](registry, ResourceTypeInstance)
	resource.RegisterResourceType[*NodeResource](registry, ResourceTypeNode)
	resource.RegisterResourceType[*SubscriptionResource](registry, ResourceTypeSubscription)
	resource.RegisterResourceType[*SyncEventResource](registry, ResourceTypeSyncEvent)
	resource.RegisterResourceType[*WaitForSyncEventResource](registry, ResourceTypeWaitForSyncEvent)
	resource.RegisterResourceType[*ReplicationSlotCreateResource](registry, ResourceTypeReplicationSlotCreate)
	resource.RegisterResourceType[*LagTrackerCommitTimestampResource](registry, ResourceTypeLagTrackerCommitTS)
	resource.RegisterResourceType[*ReplicationSlotAdvanceFromCTSResource](registry, ResourceTypeReplicationSlotAdvanceFromCTS)
	resource.RegisterResourceType[*SwitchoverResource](registry, ResourceTypeSwitchover)
}
