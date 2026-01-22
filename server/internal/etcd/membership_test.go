//go:build etcd_lifecycle_test

package etcd_test

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/stretchr/testify/assert"
)

func TestUrlsEqual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a        []string
		b        []string
		expected bool
	}{
		{
			name:     "same URLs same order",
			a:        []string{"https://192.168.1.2:2380", "https://host-1:2380"},
			b:        []string{"https://192.168.1.2:2380", "https://host-1:2380"},
			expected: true,
		},
		{
			name:     "same URLs different order",
			a:        []string{"https://192.168.1.2:2380", "https://host-1:2380"},
			b:        []string{"https://host-1:2380", "https://192.168.1.2:2380"},
			expected: true,
		},
		{
			name:     "different URLs",
			a:        []string{"https://192.168.1.2:2380"},
			b:        []string{"https://192.168.1.3:2380"},
			expected: false,
		},
		{
			name:     "different lengths",
			a:        []string{"https://192.168.1.2:2380"},
			b:        []string{"https://192.168.1.2:2380", "https://host-1:2380"},
			expected: false,
		},
		{
			name:     "both empty",
			a:        []string{},
			b:        []string{},
			expected: true,
		},
		{
			name:     "one empty",
			a:        []string{"https://192.168.1.2:2380"},
			b:        []string{},
			expected: false,
		},
		{
			name:     "both nil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "one nil one empty",
			a:        nil,
			b:        []string{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := etcd.UrlsEqual(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}
