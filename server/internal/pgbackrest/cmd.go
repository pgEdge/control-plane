package pgbackrest

import (
	"strings"

	"github.com/pgEdge/control-plane/server/internal/utils"
)

type Cmd struct {
	PgBackrestCmd string
	Config        string
	Stanza        string
	Command       string
	Args          []string
}

func (c Cmd) StringSlice() []string {
	var parts []string
	if c.PgBackrestCmd != "" {
		parts = append(parts, c.PgBackrestCmd)
	} else {
		parts = append(parts, "pgbackrest")
	}
	if c.Config != "" {
		parts = append(parts, "--config", c.Config)
	}
	if c.Stanza != "" {
		parts = append(parts, "--stanza", c.Stanza)
	}
	if c.Command != "" {
		parts = append(parts, c.Command)
	}
	parts = append(parts, c.Args...)
	return parts
}

func (c Cmd) String() string {
	return strings.Join(c.StringSlice(), " ")
}

type BackupType string

func (b BackupType) String() string {
	return string(b)
}

const (
	BackupTypeFull         BackupType = "full"
	BackupTypeDifferential BackupType = "diff"
	BackupTypeIncremental  BackupType = "incr"
)

type BackupOptions struct {
	Type          BackupType        `json:"type"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	BackupOptions map[string]string `json:"backup_options,omitempty"`
}

func (b BackupOptions) StringSlice() []string {
	options := []string{
		"--log-timestamp=n",
		"--type", b.Type.String(),
	}
	for k, v := range b.Annotations {
		options = append(options, "--annotation", k+"="+v)
	}
	backupOptions := utils.BuildOptionArgs(b.BackupOptions)
	if len(backupOptions) > 0 {
		options = append(options, backupOptions...)
	}

	return options
}
