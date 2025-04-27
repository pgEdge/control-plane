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
	DatabaseStateFailed    DatabaseState = "failed"
	DatabaseStateBackingUp DatabaseState = "backing_up"
	DatabaseStateUnknown   DatabaseState = "unknown"
)

func DatabaseStateModifiable(state DatabaseState) bool {
	switch state {
	case DatabaseStateAvailable, DatabaseStateDegraded, DatabaseStateFailed:
		return true
	default:
		return false
	}
}

type Database struct {
	DatabaseID uuid.UUID
	TenantID   *uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
	State      DatabaseState
	Spec       *Spec
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

func storedToDatabase(d *StoredDatabase, spec *Spec) *Database {
	return &Database{
		DatabaseID: d.DatabaseID,
		TenantID:   d.TenantID,
		CreatedAt:  d.CreatedAt,
		UpdatedAt:  d.UpdatedAt,
		State:      d.State,
		Spec:       spec,
	}
}
