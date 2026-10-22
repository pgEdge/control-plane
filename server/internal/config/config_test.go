package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSystemDValidate(t *testing.T) {
	valid := SystemD{
		OSFamily:       OSFamilyDebian,
		PgBackRestPath: "/usr/bin/pgbackrest",
		PatroniPath:    "/usr/bin/patroni",
	}

	for _, tc := range []struct {
		name      string
		pgBinDirs map[string]string
		expected  []string
	}{
		{
			name: "no pg_bin_dirs",
		},
		{
			name: "valid pg_bin_dirs",
			pgBinDirs: map[string]string{
				"17": "/opt/postgresql/17/bin",
				"18": "/opt/postgresql/18/bin",
			},
		},
		{
			name: "invalid pg_bin_dirs",
			pgBinDirs: map[string]string{
				"17":   "relative/bin",
				"18":   "",
				"pg18": "/opt/postgresql/18/bin",
			},
			expected: []string{
				"pg_bin_dirs.17: must be an absolute path, got 'relative/bin'",
				"pg_bin_dirs.18: must be an absolute path, got ''",
				"pg_bin_dirs: key 'pg18' must be a postgres major version, e.g. '17'",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			cfg.PgBinDirs = tc.pgBinDirs

			var errs []string
			for _, err := range cfg.validate() {
				errs = append(errs, err.Error())
			}
			assert.Equal(t, tc.expected, errs)
		})
	}
}
