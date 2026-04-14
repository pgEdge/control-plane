package resource_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

func TestVariables(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		rc := &resource.Context{
			Variables: resource.Variables{
				"foo": "bar",
			},
		}
		out, err := resource.VariableFromContext[string](rc, "foo")
		assert.NoError(t, err)
		assert.Equal(t, "bar", out)
	})

	t.Run("undefined", func(t *testing.T) {
		rc := &resource.Context{}
		out, err := resource.VariableFromContext[string](rc, "foo")
		assert.Zero(t, out)
		assert.ErrorIs(t, err, resource.ErrVariableUndefined)
	})

	t.Run("type mismatch", func(t *testing.T) {
		rc := &resource.Context{
			Variables: resource.Variables{
				"foo": "bar",
			},
		}
		out, err := resource.VariableFromContext[bool](rc, "foo")
		assert.Zero(t, out)
		assert.ErrorIs(t, err, resource.ErrVariableTypeMismatch)
		assert.ErrorContains(t, err, "variable type mismatch: expected bool, but got string")
	})
}
