package postgres

import "github.com/jackc/pgx/v5"

// spockBeta1FixVersion is the update target that Spock 6.0.0-beta2 ships to
// bring a catalog created by 6.0.0-beta1 up to date. Both betas report
// version 6.0.0, so a plain ALTER EXTENSION UPDATE does nothing on a beta1
// catalog. Remove this, and the queries that use it, at Spock 6.0.0 GA, when
// Spock removes the update target.
const spockBeta1FixVersion = "6.0.0-beta1-to-beta2"

// SpockBeta1CatalogNeedsFix reports whether the current database's Spock
// catalog was created by Spock 6.0.0-beta1 and the installed binaries ship
// the fix for it. Only the beta1 catalog lacks the spock.reserved_object
// table.
func SpockBeta1CatalogNeedsFix() Query[bool] {
	return Query[bool]{
		SQL: `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'spock' AND extversion = '6.0.0')
			AND to_regclass('spock.reserved_object') IS NULL
			AND EXISTS (
				SELECT 1 FROM pg_available_extension_versions
				WHERE name = 'spock' AND version = @fix_version
			);`,
		Args: pgx.NamedArgs{"fix_version": spockBeta1FixVersion},
	}
}

// FixSpockBeta1Catalog brings a Spock 6.0.0-beta1 catalog up to the beta2
// one, as the Spock beta2 release describes: the first update runs the fix,
// and the second only sets the version name back to 6.0.0. Both scripts are
// no-ops on a beta2 catalog.
func FixSpockBeta1Catalog() Statements {
	return Statements{
		Statement{SQL: "ALTER EXTENSION spock UPDATE TO '" + spockBeta1FixVersion + "';"},
		Statement{SQL: "ALTER EXTENSION spock UPDATE TO '6.0.0';"},
	}
}
