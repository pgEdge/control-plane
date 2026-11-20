package postgres_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/postgres"
)

func TestSplitLibraryList(t *testing.T) {
	for _, tc := range []struct {
		name        string
		list        string
		expected    []string
		expectedErr string
	}{
		{name: "empty", list: ""},
		{name: "whitespace only", list: " \t\n"},
		{name: "single name", list: "spock", expected: []string{"spock"}},
		{
			name:     "multiple names",
			list:     "pg_stat_statements,snowflake,spock",
			expected: []string{"pg_stat_statements", "snowflake", "spock"},
		},
		{
			name:     "surrounding whitespace",
			list:     " \tpg_stat_statements ,\n spock\v\f ",
			expected: []string{"pg_stat_statements", "spock"},
		},
		{
			name:     "names are not lowercased",
			list:     "Spock",
			expected: []string{"Spock"},
		},
		{
			name:     "unquoted names keep inner whitespace",
			list:     "my lib, spock",
			expected: []string{"my lib", "spock"},
		},
		{
			name:     "quoted names",
			list:     `"pg_stat_statements", "spock"`,
			expected: []string{"pg_stat_statements", "spock"},
		},
		{
			name:     "quoted name with comma",
			list:     `pg_stat_statements,"/opt/pg/my,module.so"`,
			expected: []string{"pg_stat_statements", "/opt/pg/my,module.so"},
		},
		{
			name:     "quoted name with surrounding whitespace",
			list:     `" spock "`,
			expected: []string{" spock "},
		},
		{
			name:     "quoted name with escaped quotes",
			list:     `"my ""quoted"" lib",spock`,
			expected: []string{`my "quoted" lib`, "spock"},
		},
		{
			name:     "empty quoted name",
			list:     `""`,
			expected: []string{""},
		},
		{
			name:     "path",
			list:     "$libdir/plugins/foo",
			expected: []string{"$libdir/plugins/foo"},
		},
		{name: "empty name", list: "pg_stat_statements,,spock", expectedErr: "empty name"},
		{name: "leading comma", list: ",spock", expectedErr: "empty name"},
		{name: "trailing comma", list: "spock,", expectedErr: "empty name"},
		{name: "unterminated quote", list: `"spock`, expectedErr: "unterminated quoted name"},
		{name: "text after quoted name", list: `"spock"x`, expectedErr: `unexpected text after name "spock"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := postgres.SplitLibraryList(tc.list)
			if tc.expectedErr != "" {
				assert.EqualError(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, actual)
			}
		})
	}
}
