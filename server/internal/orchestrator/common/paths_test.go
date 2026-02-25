package common_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
)

func TestInstancePaths(t *testing.T) {
	t.Run("PgBackRestRestoreCmd", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			command  string
			args     []string
			expected pgbackrest.Cmd
		}{
			{
				name:    "default",
				command: "restore",
				args:    nil,
				expected: pgbackrest.Cmd{
					PgBackrestCmd: "/usr/bin/pgbackrest",
					Config:        "/opt/pgedge/configs/pgbackrest.backup.conf",
					Stanza:        "db",
					Command:       "restore",
					Args:          nil,
				},
			},
			{
				name:    "needs target action",
				command: "restore",
				args:    []string{"--type", "immediate"},
				expected: pgbackrest.Cmd{
					PgBackrestCmd: "/usr/bin/pgbackrest",
					Config:        "/opt/pgedge/configs/pgbackrest.backup.conf",
					Stanza:        "db",
					Command:       "restore",
					Args:          []string{"--type", "immediate", "--target-action=promote"},
				},
			},
			{
				name:    "needs target action with =",
				command: "restore",
				args:    []string{"--type=name"},
				expected: pgbackrest.Cmd{
					PgBackrestCmd: "/usr/bin/pgbackrest",
					Config:        "/opt/pgedge/configs/pgbackrest.backup.conf",
					Stanza:        "db",
					Command:       "restore",
					Args:          []string{"--type=name", "--target-action=promote"},
				},
			},
			{
				name:    "already has target action",
				command: "restore",
				args:    []string{"--type=name", "--target-action", "pause"},
				expected: pgbackrest.Cmd{
					PgBackrestCmd: "/usr/bin/pgbackrest",
					Config:        "/opt/pgedge/configs/pgbackrest.backup.conf",
					Stanza:        "db",
					Command:       "restore",
					Args:          []string{"--type=name", "--target-action", "pause"},
				},
			},
			{
				name:    "already has target action with =",
				command: "restore",
				args:    []string{"--type=name", "--target-action=pause"},
				expected: pgbackrest.Cmd{
					PgBackrestCmd: "/usr/bin/pgbackrest",
					Config:        "/opt/pgedge/configs/pgbackrest.backup.conf",
					Stanza:        "db",
					Command:       "restore",
					Args:          []string{"--type=name", "--target-action=pause"},
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				paths := &common.InstancePaths{
					Instance:       common.Paths{BaseDir: "/opt/pgedge"},
					Host:           common.Paths{BaseDir: "/data/control-plane/instances/storefront-n1-689qacsi"},
					PgBackRestPath: "/usr/bin/pgbackrest",
					PatroniPath:    "/usr/bin/patroni",
				}
				result := paths.PgBackRestRestoreCmd(tc.command, tc.args...)
				assert.Equal(t, tc.expected.PgBackrestCmd, result.PgBackrestCmd)
			})
		}
	})
}
