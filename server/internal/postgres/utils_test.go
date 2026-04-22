package postgres_test

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/postgres"
	"github.com/stretchr/testify/assert"
)

func TestQuoteUnquoteIdentifier(t *testing.T) {
	for _, tc := range []struct {
		in            string
		expected      string
		nonreversible bool
	}{
		{
			in:       `foo`,
			expected: `"foo"`,
		},
		{
			in:       `foo role`,
			expected: `"foo role"`,
		},
		{
			in:       `foo "role`,
			expected: `"foo ""role"`,
		},
		{
			in:       `a"`,
			expected: `"a"""`,
		},
		{
			in:       `"a`,
			expected: `"""a"`,
		},
		{
			in:            `foo_` + string([]byte{0}) + `role`,
			expected:      `"foo_role"`,
			nonreversible: true,
		},
	} {
		t.Run(tc.in, func(t *testing.T) {
			quoted := postgres.QuoteIdentifier(tc.in)
			assert.Equal(t, tc.expected, quoted)
			if !tc.nonreversible {
				assert.Equal(t, tc.in, postgres.UnquoteIdentifier(quoted))
			}
		})
	}
}
