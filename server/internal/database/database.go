package database

import (
	"time"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

const VariableNameDatabaseNotCreated resource.VariableName = "database_not_created"

type DatabaseState string

const (
	DatabaseStateCreating  DatabaseState = "creating"
	DatabaseStateModifying DatabaseState = "modifying"
	DatabaseStateAvailable DatabaseState = "available"
	DatabaseStateDeleting  DatabaseState = "deleting"
	DatabaseStateDegraded  DatabaseState = "degraded"
	DatabaseStateFailed    DatabaseState = "failed"
	DatabaseStateRestoring DatabaseState = "restoring"
	DatabaseStateUnknown   DatabaseState = "unknown"
)

var inProgressDatabaseStates = ds.NewSet(
	DatabaseStateCreating,
	DatabaseStateModifying,
	DatabaseStateDeleting,
	DatabaseStateRestoring,
)

func (d DatabaseState) IsInProgress() bool {
	return inProgressDatabaseStates.Has(d)
}

func DatabaseStateModifiable(state DatabaseState) bool {
	switch state {
	case DatabaseStateAvailable, DatabaseStateDegraded, DatabaseStateFailed:
		return true
	default:
		return false
	}
}

type Database struct {
	DatabaseID       string
	TenantID         *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	State            DatabaseState
	Spec             *Spec
	Instances        []*Instance
	ServiceInstances []*ServiceInstance
	NotCreated       bool
}

func (d *Database) Variables() resource.Variables {
	return resource.Variables{
		VariableNameDatabaseNotCreated: d.NotCreated,
	}
}

func databaseToStored(d *Database) *StoredDatabase {
	return &StoredDatabase{
		DatabaseID: d.DatabaseID,
		TenantID:   d.TenantID,
		CreatedAt:  d.CreatedAt,
		UpdatedAt:  d.UpdatedAt,
		State:      d.State,
		NotCreated: d.NotCreated,
	}
}

func storedToDatabase(d *StoredDatabase, storedSpec *StoredSpec, instances []*Instance, serviceInstances []*ServiceInstance) *Database {
	return &Database{
		DatabaseID:       d.DatabaseID,
		TenantID:         d.TenantID,
		CreatedAt:        d.CreatedAt,
		UpdatedAt:        d.UpdatedAt,
		State:            d.State,
		Spec:             storedSpec.Spec,
		Instances:        instances,
		ServiceInstances: serviceInstances,
		NotCreated:       d.NotCreated,
	}
}

func storedToDatabases(storedDbs []*StoredDatabase, storedSpecs []*StoredSpec, allInstances []*Instance, allServiceInstances []*ServiceInstance) []*Database {
	specsByID := make(map[string]*StoredSpec, len(storedSpecs))
	for _, spec := range storedSpecs {
		specsByID[spec.DatabaseID] = spec
	}

	instancesByID := make(map[string][]*Instance, len(allInstances))
	for _, instance := range allInstances {
		instancesByID[instance.DatabaseID] = append(instancesByID[instance.DatabaseID], instance)
	}

	serviceInstancesByID := make(map[string][]*ServiceInstance, len(allServiceInstances))
	for _, serviceInstance := range allServiceInstances {
		serviceInstancesByID[serviceInstance.DatabaseID] = append(serviceInstancesByID[serviceInstance.DatabaseID], serviceInstance)
	}

	databases := make([]*Database, len(storedDbs))
	for i, stored := range storedDbs {
		spec := specsByID[stored.DatabaseID]
		instances := instancesByID[stored.DatabaseID]
		serviceInstances := serviceInstancesByID[stored.DatabaseID]
		databases[i] = storedToDatabase(stored, spec, instances, serviceInstances)
	}

	return databases
}
