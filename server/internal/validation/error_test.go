package validation_test

import (
	"errors"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/validation"
	"github.com/stretchr/testify/assert"
)

func TestValidationError(t *testing.T) {
	t.Run("with path", func(t *testing.T) {
		err := validation.NewError(errors.New("test error"), validation.NewPath(
			"array",
			validation.ArrayIndexElement(0),
			"map",
			validation.MapKeyElement("key"),
		))

		assert.ErrorContains(t, err, "array[0].map[key]: test error")
	})

	t.Run("without path", func(t *testing.T) {
		err := validation.NewError(errors.New("test error"), nil)

		assert.ErrorContains(t, err, "test error")
	})
}
