package migrations

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/resource/migrations/schemas/v0_0_0"
	"github.com/pgEdge/control-plane/server/internal/resource/migrations/schemas/v1_0_0"
)

var _ resource.StateMigration = (*Version_1_0_0)(nil)

// Version_1_0_0 adds the database.postgres_database resource type and adds the
// database name as a property to many resources.
type Version_1_0_0 struct{}

func (v *Version_1_0_0) Version() *ds.Version {
	return resource.StateVersion_1_0_0
}

func (v *Version_1_0_0) Run(state *resource.State) error {
	instances, ok := state.Resources[v0_0_0.ResourceTypeInstance]
	if !ok || len(instances) == 0 {
		// All of the changes in this version are to resources that depend on
		// instances
		return nil
	}
	nodes, ok := state.Resources[v0_0_0.ResourceTypeNode]
	if !ok || len(nodes) == 0 {
		// All of the changes in this version are to resources that depend on
		// nodes
		return nil
	}

	params, err := v.extractDatabaseParams(instances)
	if err != nil {
		return err
	}
	if params.databaseName == "" {
		return fmt.Errorf("failed to find database name")
	}
	if err := v.addDatabaseResources(state, nodes, params); err != nil {
		return err
	}
	if err := v.migrateLagTrackerResources(state, params); err != nil {
		return err
	}
	if err := v.migrateReplicationSlotAdvanceResources(state, params); err != nil {
		return err
	}
	if err := v.migrateReplicationSlotResources(state, params); err != nil {
		return err
	}
	if err := v.migrateSubscriptionResources(state, params); err != nil {
		return err
	}
	if err := v.migrateSyncEventResources(state, params); err != nil {
		return err
	}
	if err := v.migrateWaitForSyncEventResources(state, params); err != nil {
		return err
	}
	if err := v.migrationReplicationSlotCreateResource(state); err != nil {
		return err
	}

	return nil
}

type v1_0_0_databaseParams struct {
	databaseName     string
	databaseOwner    string
	renameFrom       string
	hasRestoreConfig bool
}

func (v *Version_1_0_0) extractDatabaseParams(instances map[string]*resource.ResourceData) (*v1_0_0_databaseParams, error) {
	var databaseName string
	var databaseOwner string
	var renameFrom string
	var hasRestoreConfig bool
	// Loop over instances until we find the database name
	for _, inst := range instances {
		var instance v0_0_0.InstanceResource
		if err := json.Unmarshal(inst.Attributes, &instance); err != nil {
			return nil, fmt.Errorf("failed to unmarshal instance attributes: %w", err)
		}
		if instance.Spec.DatabaseName == "" {
			continue
		}
		databaseName = instance.Spec.DatabaseName
		for _, user := range instance.Spec.DatabaseUsers {
			if user.DBOwner {
				databaseOwner = user.Username
			}
		}
		if instance.Spec.RestoreConfig != nil {
			hasRestoreConfig = true
			renameFrom = instance.Spec.RestoreConfig.SourceDatabaseName
		}
		break
	}

	return &v1_0_0_databaseParams{
		databaseName:     databaseName,
		databaseOwner:    databaseOwner,
		renameFrom:       renameFrom,
		hasRestoreConfig: hasRestoreConfig,
	}, nil
}

func (v *Version_1_0_0) addDatabaseResources(state *resource.State, nodes map[string]*resource.ResourceData, params *v1_0_0_databaseParams) error {
	for _, node := range nodes {
		nodeName := node.Identifier.ID
		dbResource := &v1_0_0.PostgresDatabaseResource{
			NodeName:         nodeName,
			DatabaseName:     params.databaseName,
			Owner:            params.databaseOwner,
			RenameFrom:       params.renameFrom,
			HasRestoreConfig: params.hasRestoreConfig,
		}
		attrs, err := json.Marshal(dbResource)
		if err != nil {
			return fmt.Errorf("failed to marshal new database resource: %w", err)
		}
		state.Add(&resource.ResourceData{
			Executor:        resource.PrimaryExecutor(nodeName),
			Identifier:      v1_0_0.PostgresDatabaseResourceIdentifier(nodeName, params.databaseName),
			Attributes:      attrs,
			Dependencies:    []resource.Identifier{node.Identifier},
			ResourceVersion: "1",
		})
	}
	return nil
}

func (v *Version_1_0_0) migrateLagTrackerResources(state *resource.State, params *v1_0_0_databaseParams) error {
	resources, ok := state.Resources[v0_0_0.ResourceTypeLagTrackerCommitTS]
	if !ok {
		return nil
	}
	// Deleting from a map while iterating is safe, but adding to it is
	// unpredictable. We'll delete old resources as we go and then add  new ones
	// back at the end.
	adds := make([]*resource.ResourceData, 0, len(resources))
	for oldID, data := range resources {
		var old v0_0_0.LagTrackerCommitTimestampResource
		if err := json.Unmarshal(data.Attributes, &old); err != nil {
			return fmt.Errorf("failed to unmarshal old lag tracker resource: %w", err)
		}
		new := v1_0_0.LagTrackerCommitTimestampResource{
			OriginNode:      old.OriginNode,
			ReceiverNode:    old.ReceiverNode,
			DatabaseName:    params.databaseName,
			CommitTimestamp: old.CommitTimestamp,
		}
		extraDeps := make([]resource.Identifier, 0, len(old.ExtraDependencies))
		for i, dep := range old.ExtraDependencies {
			if dep.Type != v0_0_0.ResourceTypeWaitForSyncEvent.String() {
				return fmt.Errorf("unexpected lag tracker extra dependency type: type=%s, id=%s", dep.Type, dep.ID)
			}
			sourceNode := strings.TrimSuffix(dep.ID, new.ReceiverNode)
			if dep.ID != v0_0_0.WaitForSyncEventResourceIdentifier(sourceNode, new.ReceiverNode).ID {
				return fmt.Errorf("unexpected lag tracker extra dependency id: type=%s, id=%s", dep.Type, dep.ID)
			}
			extraDeps = append(extraDeps, v1_0_0.WaitForSyncEventResourceIdentifier(sourceNode, new.ReceiverNode, new.DatabaseName))
			new.ExtraDependencies = append(new.ExtraDependencies, struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			}{
				ID:   extraDeps[i].ID,
				Type: extraDeps[i].Type.String(),
			})
		}
		attrs, err := json.Marshal(new)
		if err != nil {
			return fmt.Errorf("failed to marshal new lag tracker resource: %w", err)
		}
		adds = append(adds, &resource.ResourceData{
			Identifier: v1_0_0.LagTrackerCommitTSIdentifier(new.OriginNode, new.ReceiverNode, new.DatabaseName),
			Attributes: attrs,
			Dependencies: slices.Concat(
				[]resource.Identifier{
					v1_0_0.PostgresDatabaseResourceIdentifier(new.ReceiverNode, new.DatabaseName),
					v1_0_0.PostgresDatabaseResourceIdentifier(new.OriginNode, new.DatabaseName),
				},
				extraDeps,
			),
			Executor:         data.Executor,
			ResourceVersion:  data.ResourceVersion,
			NeedsRecreate:    data.NeedsRecreate,
			DiffIgnore:       data.DiffIgnore,
			PendingDeletion:  data.PendingDeletion,
			Error:            data.Error,
			TypeDependencies: data.TypeDependencies,
		})
		delete(resources, oldID)
	}
	state.Add(adds...)

	return nil
}

func (v *Version_1_0_0) migrateReplicationSlotAdvanceResources(state *resource.State, params *v1_0_0_databaseParams) error {
	resources, ok := state.Resources[v0_0_0.ResourceTypeReplicationSlotAdvanceFromCTS]
	if !ok {
		return nil
	}
	adds := make([]*resource.ResourceData, 0, len(resources))
	for oldID, data := range resources {
		var old v0_0_0.ReplicationSlotAdvanceFromCTSResource
		if err := json.Unmarshal(data.Attributes, &old); err != nil {
			return fmt.Errorf("failed to unmarshal old replication slot advance resource: %w", err)
		}
		new := v1_0_0.ReplicationSlotAdvanceFromCTSResource{
			ProviderNode:   old.ProviderNode,
			SubscriberNode: old.SubscriberNode,
			DatabaseName:   params.databaseName,
		}
		attrs, err := json.Marshal(new)
		if err != nil {
			return fmt.Errorf("failed to marshal new replication slot advance resource: %w", err)
		}
		adds = append(adds, &resource.ResourceData{
			Identifier: v1_0_0.ReplicationSlotAdvanceFromCTSResourceIdentifier(new.ProviderNode, new.SubscriberNode, new.DatabaseName),
			Attributes: attrs,
			Dependencies: []resource.Identifier{
				v1_0_0.PostgresDatabaseResourceIdentifier(new.ProviderNode, new.DatabaseName),
				v1_0_0.PostgresDatabaseResourceIdentifier(new.SubscriberNode, new.DatabaseName),
				v1_0_0.LagTrackerCommitTSIdentifier(new.ProviderNode, new.SubscriberNode, new.DatabaseName),
			},
			Executor:         data.Executor,
			ResourceVersion:  data.ResourceVersion,
			NeedsRecreate:    data.NeedsRecreate,
			DiffIgnore:       data.DiffIgnore,
			PendingDeletion:  data.PendingDeletion,
			Error:            data.Error,
			TypeDependencies: data.TypeDependencies,
		})
		delete(resources, oldID)
	}
	state.Add(adds...)

	return nil
}

func (v *Version_1_0_0) migrateReplicationSlotResources(state *resource.State, params *v1_0_0_databaseParams) error {
	resources, ok := state.Resources[v0_0_0.ResourceTypeReplicationSlot]
	if !ok {
		return nil
	}
	adds := make([]*resource.ResourceData, 0, len(resources))
	for oldID, data := range resources {
		var old v0_0_0.ReplicationSlotResource
		if err := json.Unmarshal(data.Attributes, &old); err != nil {
			return fmt.Errorf("failed to unmarshal old replication slot resource: %w", err)
		}
		new := v1_0_0.ReplicationSlotResource{
			ProviderNode:   old.ProviderNode,
			SubscriberNode: old.SubscriberNode,
			DatabaseName:   params.databaseName,
		}
		attrs, err := json.Marshal(new)
		if err != nil {
			return fmt.Errorf("failed to marshal new replication slot resource: %w", err)
		}
		adds = append(adds, &resource.ResourceData{
			Identifier: v1_0_0.ReplicationSlotResourceIdentifier(new.ProviderNode, new.SubscriberNode, new.DatabaseName),
			Attributes: attrs,
			Dependencies: []resource.Identifier{
				v1_0_0.PostgresDatabaseResourceIdentifier(new.ProviderNode, new.DatabaseName),
			},
			Executor:         data.Executor,
			ResourceVersion:  data.ResourceVersion,
			NeedsRecreate:    data.NeedsRecreate,
			DiffIgnore:       data.DiffIgnore,
			PendingDeletion:  data.PendingDeletion,
			Error:            data.Error,
			TypeDependencies: data.TypeDependencies,
		})
		delete(resources, oldID)
	}
	state.Add(adds...)

	return nil
}

func (v *Version_1_0_0) migrateSubscriptionResources(state *resource.State, params *v1_0_0_databaseParams) error {
	resources, ok := state.Resources[v0_0_0.ResourceTypeSubscription]
	if !ok {
		return nil
	}
	adds := make([]*resource.ResourceData, 0, len(resources))
	for oldID, data := range resources {
		var old v0_0_0.SubscriptionResource
		if err := json.Unmarshal(data.Attributes, &old); err != nil {
			return fmt.Errorf("failed to unmarshal old subscription resource: %w", err)
		}
		new := v1_0_0.SubscriptionResource{
			DatabaseName:   params.databaseName,
			SubscriberNode: old.SubscriberNode,
			ProviderNode:   old.ProviderNode,
			Disabled:       old.Disabled,
			SyncStructure:  old.SyncStructure,
			SyncData:       old.SyncData,
			NeedsUpdate:    old.NeedsUpdate,
		}
		extraDeps := make([]resource.Identifier, 0, len(old.ExtraDependencies))
		for i, dep := range old.ExtraDependencies {
			if dep.Type != v0_0_0.ResourceTypeWaitForSyncEvent.String() {
				return fmt.Errorf("unexpected subscription extra dependency type: type=%s, id=%s", dep.Type, dep.ID)
			}
			peerNode := strings.TrimSuffix(dep.ID, new.ProviderNode)
			if dep.ID != v0_0_0.WaitForSyncEventResourceIdentifier(peerNode, new.ProviderNode).ID {
				return fmt.Errorf("unexpected subscription extra dependency id: type=%s, id=%s", dep.Type, dep.ID)
			}
			extraDeps = append(extraDeps, v1_0_0.WaitForSyncEventResourceIdentifier(peerNode, new.ProviderNode, new.DatabaseName))
			new.ExtraDependencies = append(new.ExtraDependencies, struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			}{
				ID:   extraDeps[i].ID,
				Type: extraDeps[i].Type.String(),
			})
		}
		attrs, err := json.Marshal(new)
		if err != nil {
			return fmt.Errorf("failed to marshal new subscription resource: %w", err)
		}
		adds = append(adds, &resource.ResourceData{
			Identifier: v1_0_0.SubscriptionResourceIdentifier(new.ProviderNode, new.SubscriberNode, new.DatabaseName),
			Attributes: attrs,
			Dependencies: slices.Concat(
				[]resource.Identifier{
					v1_0_0.PostgresDatabaseResourceIdentifier(new.SubscriberNode, new.DatabaseName),
					v1_0_0.PostgresDatabaseResourceIdentifier(new.ProviderNode, new.DatabaseName),
					v1_0_0.ReplicationSlotResourceIdentifier(new.ProviderNode, new.SubscriberNode, new.DatabaseName),
				},
				extraDeps,
			),
			Executor:         data.Executor,
			ResourceVersion:  data.ResourceVersion,
			NeedsRecreate:    data.NeedsRecreate,
			DiffIgnore:       data.DiffIgnore,
			PendingDeletion:  data.PendingDeletion,
			Error:            data.Error,
			TypeDependencies: data.TypeDependencies,
		})
		delete(resources, oldID)
	}
	state.Add(adds...)

	return nil
}

func (v *Version_1_0_0) migrateSyncEventResources(state *resource.State, params *v1_0_0_databaseParams) error {
	resources, ok := state.Resources[v0_0_0.ResourceTypeSyncEvent]
	if !ok {
		return nil
	}
	adds := make([]*resource.ResourceData, 0, len(resources))
	for oldID, data := range resources {
		var old v0_0_0.SyncEventResource
		if err := json.Unmarshal(data.Attributes, &old); err != nil {
			return fmt.Errorf("failed to unmarshal old sync event resource: %w", err)
		}
		new := v1_0_0.SyncEventResource{
			DatabaseName:   params.databaseName,
			SubscriberNode: old.SubscriberNode,
			ProviderNode:   old.ProviderNode,
			SyncEventLsn:   old.SyncEventLsn,
		}
		extraDeps := make([]resource.Identifier, 0, len(old.ExtraDependencies))
		for _, dep := range old.ExtraDependencies {
			switch dep.Type {
			case v0_0_0.ResourceTypeReplicationSlotCreate.String():
				// This resource identifier is unchanged
				extraDeps = append(extraDeps, resource.Identifier{
					ID:   dep.ID,
					Type: resource.Type(dep.Type),
				})
				new.ExtraDependencies = append(new.ExtraDependencies, dep)
			case v0_0_0.ResourceTypeSubscription.String():
				if dep.ID != v0_0_0.SubscriptionResourceIdentifier(new.ProviderNode, new.SubscriberNode).ID {
					return fmt.Errorf("unexpected sync event extra dependency id: type=%s, id=%s", dep.Type, dep.ID)
				}
				// This resource did not need to be in the extra dependencies
				// because it was already in the normal dependencies. We can
				// elide it.
			default:
				return fmt.Errorf("unexpected sync event extra dependency type: type=%s, id=%s", dep.Type, dep.ID)
			}
		}
		attrs, err := json.Marshal(new)
		if err != nil {
			return fmt.Errorf("failed to marshal sync event resource: %w", err)
		}
		adds = append(adds, &resource.ResourceData{
			Identifier: v1_0_0.SyncEventResourceIdentifier(new.ProviderNode, new.SubscriberNode, new.DatabaseName),
			Attributes: attrs,
			Dependencies: slices.Concat(
				[]resource.Identifier{
					v1_0_0.SubscriptionResourceIdentifier(new.ProviderNode, new.SubscriberNode, new.DatabaseName),
				},
				extraDeps,
			),
			Executor:         data.Executor,
			ResourceVersion:  data.ResourceVersion,
			NeedsRecreate:    data.NeedsRecreate,
			DiffIgnore:       data.DiffIgnore,
			PendingDeletion:  data.PendingDeletion,
			Error:            data.Error,
			TypeDependencies: data.TypeDependencies,
		})
		delete(resources, oldID)
	}
	state.Add(adds...)

	return nil
}

func (v *Version_1_0_0) migrateWaitForSyncEventResources(state *resource.State, params *v1_0_0_databaseParams) error {
	resources, ok := state.Resources[v0_0_0.ResourceTypeWaitForSyncEvent]
	if !ok {
		return nil
	}
	adds := make([]*resource.ResourceData, 0, len(resources))
	for oldID, data := range resources {
		var old v0_0_0.WaitForSyncEventResource
		if err := json.Unmarshal(data.Attributes, &old); err != nil {
			return fmt.Errorf("failed to unmarshal old wait for sync event resource: %w", err)
		}
		new := v1_0_0.WaitForSyncEventResource{
			DatabaseName:   params.databaseName,
			SubscriberNode: old.SubscriberNode,
			ProviderNode:   old.ProviderNode,
		}
		attrs, err := json.Marshal(new)
		if err != nil {
			return fmt.Errorf("failed to marshal wait for sync event resource: %w", err)
		}
		adds = append(adds, &resource.ResourceData{
			Identifier: v1_0_0.WaitForSyncEventResourceIdentifier(new.ProviderNode, new.SubscriberNode, new.DatabaseName),
			Attributes: attrs,
			Dependencies: []resource.Identifier{
				v1_0_0.SyncEventResourceIdentifier(new.ProviderNode, new.SubscriberNode, new.DatabaseName),
			},
			Executor:         data.Executor,
			ResourceVersion:  data.ResourceVersion,
			NeedsRecreate:    data.NeedsRecreate,
			DiffIgnore:       data.DiffIgnore,
			PendingDeletion:  data.PendingDeletion,
			Error:            data.Error,
			TypeDependencies: data.TypeDependencies,
		})
		delete(resources, oldID)
	}
	state.Add(adds...)

	return nil
}

func (v *Version_1_0_0) migrationReplicationSlotCreateResource(state *resource.State) error {
	resources, ok := state.Resources[v0_0_0.ResourceTypeReplicationSlotCreate]
	if !ok {
		return nil
	}
	adds := make([]*resource.ResourceData, 0, len(resources))
	for _, data := range resources {
		var attrs v0_0_0.ReplicationSlotCreateResource
		if err := json.Unmarshal(data.Attributes, &attrs); err != nil {
			return fmt.Errorf("failed to unmarshal old replication slot create resource: %w", err)
		}
		// This Add will overwrite the old resource since the IDs haven't
		// changed, so there's no need to do delete the old resource in this
		// method.
		adds = append(adds, &resource.ResourceData{
			Identifier: data.Identifier, // This identifier hasn't changed
			Attributes: data.Attributes, // The attributes haven't changed
			Dependencies: []resource.Identifier{
				v1_0_0.PostgresDatabaseResourceIdentifier(attrs.ProviderNode, attrs.DatabaseName),
			},
			Executor:         data.Executor,
			ResourceVersion:  data.ResourceVersion,
			NeedsRecreate:    data.NeedsRecreate,
			DiffIgnore:       data.DiffIgnore,
			PendingDeletion:  data.PendingDeletion,
			Error:            data.Error,
			TypeDependencies: data.TypeDependencies,
		})
	}
	state.Add(adds...)

	return nil
}
