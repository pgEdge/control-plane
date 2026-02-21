package postgres_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/postgres"
)

func TestIsSpockNodeNotConfigured(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name: "returns true for SQLSTATE 55000",
			err: &pgconn.PgError{
				Code: pgerrcode.ObjectNotInPrerequisiteState, // "55000"
			},
			expected: true,
		},
		{
			name: "returns false for unrelated SQLSTATE",
			err: &pgconn.PgError{
				Code: pgerrcode.UndefinedTable, // "42P01"
			},
			expected: false,
		},
		{
			name:     "returns false for a non-postgres error",
			err:      errors.New("some generic error"),
			expected: false,
		},
		{
			name:     "returns false for nil",
			err:      nil,
			expected: false,
		},
		{
			name:     "returns true when wrapped with fmt.Errorf",
			err:      fmt.Errorf("outer: %w", &pgconn.PgError{Code: pgerrcode.ObjectNotInPrerequisiteState}),
			expected: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, postgres.IsSpockNodeNotConfigured(tc.err))
		})
	}
}
