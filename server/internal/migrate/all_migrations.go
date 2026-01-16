package migrate

import (
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/migrate/migrations"
)

// AllMigrations returns the ordered list of migrations.
// Order matters - migrations are executed in slice order.
// Add new migrations to this list in chronological order.
func AllMigrations() ([]Migration, error) {
	all := []Migration{
		&migrations.AddTaskScope{},
	}

	// Validate that migration identifiers are unique.
	seenIDs := ds.NewSet[string]()
	for _, migration := range all {
		id := migration.Identifier()
		if seenIDs.Has(id) {
			return nil, fmt.Errorf("duplicate identifier in migrations list: %s", id)
		}
		seenIDs.Add(id)
	}

	return all, nil
}
