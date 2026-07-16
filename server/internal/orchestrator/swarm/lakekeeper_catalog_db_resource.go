package swarm

import (
	"fmt"
	"net/url"
	"unicode/utf8"

	"github.com/pgEdge/control-plane/server/internal/database"
)

// lakekeeperCatalogDBName derives the managed catalog database name for
// a database. Kept within Postgres's 63-byte identifier limit by
// truncating the database-name prefix, never the suffix. Truncation
// backs off to the last whole UTF-8 rune so it never leaves a partial
// multi-byte rune, which would be an invalid identifier on a UTF8
// database.
func lakekeeperCatalogDBName(databaseName string) string {
	const suffix = "_lakekeeper"
	const maxLen = 63
	if limit := maxLen - len(suffix); len(databaseName) > limit {
		databaseName = databaseName[:limit]
		for len(databaseName) > 0 && !utf8.ValidString(databaseName) {
			databaseName = databaseName[:len(databaseName)-1]
		}
	}
	return databaseName + suffix
}

// buildManagedCatalogDBURL constructs the catalog Postgres URL for a
// control-plane-managed catalog, using the overlay-network host entry
// and the service's connect-as credentials. The result contains a
// password: never log it.
func buildManagedCatalogDBURL(host database.ServiceHostEntry, username, password, dbName string) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(username, password),
		Host:   fmt.Sprintf("%s:%d", host.Host, host.Port),
		Path:   "/" + dbName,
	}
	return u.String()
}
