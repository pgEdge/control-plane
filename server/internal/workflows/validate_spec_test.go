package workflows

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/database"
)

func TestValidateSpecOutput_merge_warnings(t *testing.T) {
	t.Run("warnings collected from results", func(t *testing.T) {
		out := &ValidateSpecOutput{Valid: true}
		out.merge([]*database.ValidationResult{
			{Valid: true, NodeName: "n1", HostID: "host-1", Warnings: []string{"image X is not in manifest"}},
			{Valid: true, NodeName: "n1", HostID: "host-2", Warnings: []string{"image X is not in manifest"}},
		})

		assert.True(t, out.Valid, "warnings must not mark output invalid")
		assert.Len(t, out.Warnings, 2)
		assert.Contains(t, out.Warnings[0], "n1")
		assert.Contains(t, out.Warnings[0], "host-1")
		assert.Contains(t, out.Warnings[0], "image X is not in manifest")
		assert.Empty(t, out.Errors)
	})

	t.Run("errors and warnings can coexist", func(t *testing.T) {
		out := &ValidateSpecOutput{Valid: true}
		out.merge([]*database.ValidationResult{
			{Valid: false, NodeName: "n1", HostID: "host-1", Errors: []string{"port in use"}},
			{Valid: true, NodeName: "n1", HostID: "host-2", Warnings: []string{"custom image"}},
		})

		assert.False(t, out.Valid)
		assert.Len(t, out.Errors, 1)
		assert.Len(t, out.Warnings, 1)
	})

	t.Run("no warnings when Image not set", func(t *testing.T) {
		out := &ValidateSpecOutput{Valid: true}
		out.merge([]*database.ValidationResult{
			{Valid: true, NodeName: "n1", HostID: "host-1"},
		})

		assert.True(t, out.Valid)
		assert.Empty(t, out.Warnings)
		assert.Empty(t, out.Errors)
	})
}
