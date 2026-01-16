package migrate

import (
	"context"

	"github.com/samber/do"
)

// Migration defines the interface for data migrations.
type Migration interface {
	// Identifier returns a unique semantic name for this migration.
	Identifier() string
	// Run executes the migration using dependencies from the injector.
	// The context should be used for cancellation and timeouts.
	Run(ctx context.Context, i *do.Injector) error
}
