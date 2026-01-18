package task_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/task"
)

func TestScopeString(t *testing.T) {
	assert.Equal(t, "database", task.ScopeDatabase.String())
	assert.Equal(t, "host", task.ScopeHost.String())
}

func TestOptionsValidation(t *testing.T) {
	tests := []struct {
		name        string
		opts        task.Options
		expectError string
	}{
		{
			name: "valid database scope",
			opts: task.Options{
				Scope:      task.ScopeDatabase,
				DatabaseID: "my-database",
				Type:       task.TypeCreate,
			},
			expectError: "",
		},
		{
			name: "valid host scope",
			opts: task.Options{
				Scope:  task.ScopeHost,
				HostID: "host-1",
				Type:   task.TypeRemoveHost,
			},
			expectError: "",
		},
		{
			name: "missing scope",
			opts: task.Options{
				DatabaseID: "my-database",
				Type:       task.TypeCreate,
			},
			expectError: "scope is required",
		},
		{
			name: "missing type",
			opts: task.Options{
				Scope:      task.ScopeDatabase,
				DatabaseID: "my-database",
			},
			expectError: "type is required",
		},
		{
			name: "database scope missing database_id",
			opts: task.Options{
				Scope: task.ScopeDatabase,
				Type:  task.TypeCreate,
			},
			expectError: "database_id is required for database scope",
		},
		{
			name: "host scope missing host_id",
			opts: task.Options{
				Scope: task.ScopeHost,
				Type:  task.TypeRemoveHost,
			},
			expectError: "host_id is required for host scope",
		},
		{
			name: "invalid scope",
			opts: task.Options{
				Scope: task.Scope("invalid"),
				Type:  task.TypeCreate,
			},
			expectError: "invalid scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := task.NewTask(tt.opts)
			if tt.expectError == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectError)
			}
		})
	}
}
