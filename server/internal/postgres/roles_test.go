package postgres_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/postgres"
)

func TestCreateUserRole(t *testing.T) {
	for _, tc := range []struct {
		name        string
		opts        postgres.UserRoleOptions
		expected    postgres.Statements
		expectedErr string
	}{
		{
			name: "app user",
			opts: postgres.UserRoleOptions{
				Name:       "app",
				Password:   "password",
				DBName:     "northwind",
				Attributes: []string{"LOGIN"},
				Roles:      []string{"pgedge_application"},
			},
			expected: postgres.Statements{
				postgres.ConditionalStatement{
					If: postgres.Query[bool]{
						SQL: `SELECT NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = @name);`,
						Args: pgx.NamedArgs{
							"name": "app",
						},
					},
					Then: postgres.Statement{
						SQL: `CREATE ROLE "app"`,
					},
				},
				postgres.Statement{SQL: `ALTER ROLE "app" WITH PASSWORD 'password';`},
				postgres.Statement{SQL: `ALTER ROLE "app" WITH LOGIN;`},
				postgres.Statement{SQL: `GRANT "pgedge_application" TO "app" WITH INHERIT TRUE;`},
			},
		},
		{
			name: "DE admin user",
			opts: postgres.UserRoleOptions{
				Name:       "admin",
				Password:   "password",
				DBName:     "northwind",
				DBOwner:    true,
				Attributes: []string{"LOGIN", "CREATEDB", "CREATEROLE"},
				Roles:      []string{"pgedge_superuser"},
			},
			expected: postgres.Statements{
				postgres.ConditionalStatement{
					If: postgres.Query[bool]{
						SQL: `SELECT NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = @name);`,
						Args: pgx.NamedArgs{
							"name": "admin",
						},
					},
					Then: postgres.Statement{SQL: `CREATE ROLE "admin"`},
				},
				postgres.Statement{SQL: `ALTER ROLE "admin" WITH PASSWORD 'password';`},
				postgres.Statement{SQL: `ALTER ROLE "admin" WITH LOGIN;`},
				postgres.Statement{SQL: `ALTER ROLE "admin" WITH CREATEDB;`},
				postgres.Statement{SQL: `ALTER ROLE "admin" WITH CREATEROLE;`},
				postgres.Statement{SQL: `ALTER DATABASE "northwind" OWNER TO "admin";`},
				postgres.Statement{SQL: `GRANT "pgedge_superuser" TO "admin" WITH INHERIT TRUE;`},
			},
		},
		{
			name: "EE admin user",
			opts: postgres.UserRoleOptions{
				Name:       "admin",
				Password:   "password",
				DBName:     "northwind",
				DBOwner:    true,
				Attributes: []string{"LOGIN", "SUPERUSER"},
			},
			expected: postgres.Statements{
				postgres.ConditionalStatement{
					If: postgres.Query[bool]{
						SQL: `SELECT NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = @name);`,
						Args: pgx.NamedArgs{
							"name": "admin",
						},
					},
					Then: postgres.Statement{SQL: `CREATE ROLE "admin"`},
				},
				postgres.Statement{SQL: `ALTER ROLE "admin" WITH PASSWORD 'password';`},
				postgres.Statement{SQL: `ALTER ROLE "admin" WITH LOGIN;`},
				postgres.Statement{SQL: `ALTER ROLE "admin" WITH SUPERUSER;`},
				postgres.Statement{SQL: `ALTER DATABASE "northwind" OWNER TO "admin";`},
			},
		},
		{
			name: "role conflict",
			opts: postgres.UserRoleOptions{
				Name: "pgedge_application",
			},
			expectedErr: `conflicts with a builtin role`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := postgres.CreateUserRole(tc.opts)
			if tc.expectedErr != "" {
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, actual)
			}
		})
	}
}
