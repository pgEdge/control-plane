package apiv1

import (
	"testing"

	"github.com/stretchr/testify/assert"

	genserver "github.com/pgEdge/control-plane/api/apiv1/gen/http/control_plane/server"
)

// PLAT-706: the node name pattern must match the whole name. Unanchored, it
// accepted any name that merely contained "n" followed by a digit, such as
// "n1-east", which then failed later with a server error.
func TestNodeNamePatternIsAnchored(t *testing.T) {
	validate := map[string]func(name string) error{
		"create body": func(name string) error {
			return genserver.ValidateDatabaseNodeSpecRequestBody(&genserver.DatabaseNodeSpecRequestBody{
				Name:    &name,
				HostIds: []string{"host-1"},
			})
		},
		"update body": func(name string) error {
			return genserver.ValidateDatabaseNodeSpecRequestBodyRequestBody(&genserver.DatabaseNodeSpecRequestBodyRequestBody{
				Name:    &name,
				HostIds: []string{"host-1"},
			})
		},
	}

	for body, fn := range validate {
		for _, name := range []string{"n1", "n10", "n01"} {
			t.Run(body+" accepts "+name, func(t *testing.T) {
				assert.NoError(t, fn(name))
			})
		}
		for _, name := range []string{"n1-east", "an1", "nodes-n2", "n1a", "analytics-1", "n"} {
			t.Run(body+" rejects "+name, func(t *testing.T) {
				assert.ErrorContains(t, fn(name), `must match the regexp "^n[0-9]+$"`)
			})
		}
	}
}
