package swarm_test

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/orchestrator/swarm"
	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
	"github.com/stretchr/testify/assert"
)

func TestPgBackRestRestoreCmd(t *testing.T) {
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
			result := swarm.PgBackRestRestoreCmd(tc.command, tc.args...)
			assert.Equal(t, tc.expected.PgBackrestCmd, result.PgBackrestCmd)
		})
	}
}
