package postgres

import (
	"maps"
	"slices"
	"strings"
)

type PgServiceConf struct {
	Services map[string]*DSN
}

func NewPgServiceConf() *PgServiceConf {
	return &PgServiceConf{
		Services: map[string]*DSN{},
	}
}

func (c *PgServiceConf) String() string {
	var buf strings.Builder

	// Always write in a consistent order
	keys := slices.Sorted(maps.Keys(c.Services))

	// Not using gopkg.in/ini here because Postgres does not like pretty spaces
	// and the ini library uses a global variable to configure pretty spaces, so
	// it would interfere with the pgBackRest conf.
	for _, key := range keys {
		buf.WriteString("[")
		buf.WriteString(key)
		buf.WriteString("]\n")

		for _, field := range c.Services[key].Fields() {
			buf.WriteString(field)
			buf.WriteString("\n")
		}

		buf.WriteString("\n")
	}

	return buf.String()
}
