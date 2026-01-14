package migrate

import "github.com/pgEdge/control-plane/server/internal/migrate/migrations"

// allMigrations returns the ordered list of migrations.
// Order matters - migrations are executed in slice order.
// Add new migrations to this list in chronological order.
func allMigrations() []Migration {
	return []Migration{
		&migrations.AddTaskScope{},
	}
}
