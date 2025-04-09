package pgbackrest

import "strings"

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
