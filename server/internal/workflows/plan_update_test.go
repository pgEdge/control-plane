package workflows

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/database"
)

func TestResolveTargetSessionAttrs(t *testing.T) {
	tests := []struct {
		name     string
		spec     *database.ServiceSpec
		expected string
	}{
		{
			name: "explicit target_session_attrs overrides everything",
			spec: &database.ServiceSpec{
				ServiceType: "mcp",
				Config:      map[string]any{"allow_writes": true},
				DatabaseConnection: &database.DatabaseConnection{
					TargetSessionAttrs: database.TargetSessionAttrsStandby,
				},
			},
			expected: database.TargetSessionAttrsStandby,
		},
		{
			name: "MCP with allow_writes true returns primary",
			spec: &database.ServiceSpec{
				ServiceType: "mcp",
				Config:      map[string]any{"allow_writes": true},
			},
			expected: database.TargetSessionAttrsPrimary,
		},
		{
			name: "MCP with allow_writes false returns prefer-standby",
			spec: &database.ServiceSpec{
				ServiceType: "mcp",
				Config:      map[string]any{"allow_writes": false},
			},
			expected: database.TargetSessionAttrsPreferStandby,
		},
		{
			name: "MCP with no allow_writes in config returns prefer-standby",
			spec: &database.ServiceSpec{
				ServiceType: "mcp",
				Config:      map[string]any{"other_key": "value"},
			},
			expected: database.TargetSessionAttrsPreferStandby,
		},
		{
			name: "MCP with nil config returns prefer-standby",
			spec: &database.ServiceSpec{
				ServiceType: "mcp",
				Config:      nil,
			},
			expected: database.TargetSessionAttrsPreferStandby,
		},
		{
			name: "unknown service type returns prefer-standby",
			spec: &database.ServiceSpec{
				ServiceType: "unknown-type",
				Config:      map[string]any{"allow_writes": true},
			},
			expected: database.TargetSessionAttrsPreferStandby,
		},
		{
			name: "explicit override wins over MCP allow_writes=true",
			spec: &database.ServiceSpec{
				ServiceType: "mcp",
				Config:      map[string]any{"allow_writes": true},
				DatabaseConnection: &database.DatabaseConnection{
					TargetSessionAttrs: database.TargetSessionAttrsAny,
				},
			},
			expected: database.TargetSessionAttrsAny,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveTargetSessionAttrs(tc.spec)
			assert.Equal(t, tc.expected, got)
		})
	}
}
