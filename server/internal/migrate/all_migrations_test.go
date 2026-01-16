package migrate_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/migrate"
)

func TestAllMigrations(t *testing.T) {
	_, err := migrate.AllMigrations()
	require.NoError(t, err)
}
