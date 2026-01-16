package migrate

// allMigrations returns the ordered list of migrations.
// Order matters - migrations are executed in slice order.
// Add new migrations to this list in chronological order.
func allMigrations() []Migration {
	return []Migration{
		// Add migrations here in chronological order
		// Example:
		// &AddHostMetadataField{},
		// &RenameDatabaseStatus{},
	}
}
