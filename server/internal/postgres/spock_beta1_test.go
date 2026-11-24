package postgres_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/postgres"
)

func TestFixSpockBeta1Catalog(t *testing.T) {
	var sql []string
	for _, stmt := range postgres.FixSpockBeta1Catalog() {
		sql = append(sql, stmt.(postgres.Statement).SQL)
	}
	assert.Equal(t, []string{
		"ALTER EXTENSION spock UPDATE TO '6.0.0-beta1-to-beta2';",
		"ALTER EXTENSION spock UPDATE TO '6.0.0';",
	}, sql)

	check := postgres.SpockBeta1CatalogNeedsFix()
	assert.Contains(t, check.SQL, "to_regclass('spock.reserved_object') IS NULL")
	assert.Equal(t, "6.0.0-beta1-to-beta2", check.Args["fix_version"])
}
