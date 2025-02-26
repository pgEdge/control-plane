package database

import (
	"time"

	"github.com/google/uuid"
)

type DatabaseState string

const (
	DatabaseStateCreating  DatabaseState = "creating"
	DatabaseStateModifying DatabaseState = "modifying"
	DatabaseStateAvailable DatabaseState = "available"
	DatabaseStateDeleting  DatabaseState = "deleting"
	DatabaseStateDegraded  DatabaseState = "degraded"
	DatabaseStateUnknown   DatabaseState = "unknown"
)

type Database struct {
	DatabaseID uuid.UUID
	TenantID   *uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
	State      DatabaseState
	Spec       *Spec
	Instances  []*Instance
}

func databaseToStored(d *Database) *StoredDatabase {
	return &StoredDatabase{
		DatabaseID: d.DatabaseID,
		TenantID:   d.TenantID,
		CreatedAt:  d.CreatedAt,
		UpdatedAt:  d.UpdatedAt,
		State:      d.State,
	}
}

// func storedToDatabase(d *StoredDatabase, instances []*Instance) *Database {
// 	return &Database{
// 		DatabaseID: d.DatabaseID,
// 		TenantID:   d.TenantID,
// 		CreatedAt:  d.CreatedAt,
// 		UpdatedAt:  d.UpdatedAt,
// 		State:      d.State,
// 		Instances:  instances,
// 	}
// }
