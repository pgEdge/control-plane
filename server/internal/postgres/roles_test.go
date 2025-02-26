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
				{SQL: `CREATE ROLE "app"`},
				{
					SQL:  `ALTER ROLE "app" WITH PASSWORD @password;`,
					Args: pgx.NamedArgs{"password": "password"},
				},
				{SQL: `ALTER ROLE "app" WITH "LOGIN";`},
				{SQL: `GRANT "pgedge_application" TO "app" WITH INHERIT TRUE;`},
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
				{SQL: `CREATE ROLE "admin"`},
				{
					SQL:  `ALTER ROLE "admin" WITH PASSWORD @password;`,
					Args: pgx.NamedArgs{"password": "password"},
				},
				{SQL: `ALTER ROLE "admin" WITH "LOGIN";`},
				{SQL: `ALTER ROLE "admin" WITH "CREATEDB";`},
				{SQL: `ALTER ROLE "admin" WITH "CREATEROLE";`},
				{SQL: `ALTER DATABASE "northwind" OWNER TO "admin";`},
				{SQL: `GRANT "pgedge_superuser" TO "admin" WITH INHERIT TRUE;`},
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
				{SQL: `CREATE ROLE "admin"`},
				{
					SQL:  `ALTER ROLE "admin" WITH PASSWORD @password;`,
					Args: pgx.NamedArgs{"password": "password"},
				},
				{SQL: `ALTER ROLE "admin" WITH "LOGIN";`},
				{SQL: `ALTER ROLE "admin" WITH "SUPERUSER";`},
				{SQL: `ALTER DATABASE "northwind" OWNER TO "admin";`},
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
