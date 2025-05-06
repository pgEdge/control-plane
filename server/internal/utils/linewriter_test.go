package utils_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/utils"
)

func TestLineWriter(t *testing.T) {
	for _, tc := range []struct {
		name        string
		writes      [][]byte
		expectEmits []string
	}{
		{
			name: "basic emission with newlines",
			writes: [][]byte{
				[]byte("hello\nworld\n"),
			},
			expectEmits: []string{"hello", "world"},
		},
		{
			name: "partial writes",
			writes: [][]byte{
				[]byte("foo"),
				[]byte("bar\nbaz"),
				[]byte("qux\n"),
			},
			expectEmits: []string{"foobar", "bazqux"},
		},
		{
			name: "flush without newline",
			writes: [][]byte{
				[]byte("incomplete line"),
			},
			expectEmits: []string{"incomplete line"},
		},
		{
			name: "multiple newlines in one write",
			writes: [][]byte{
				[]byte("a\nb\nc\n"),
			},
			expectEmits: []string{"a", "b", "c"},
		},
		{
			name: "empty write",
			writes: [][]byte{
				[]byte(""),
			},
			expectEmits: []string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var emitted [][]byte

			writer := utils.NewLineWriter(func(data []byte) error {
				cpy := make([]byte, len(data))
				copy(cpy, data)
				emitted = append(emitted, cpy)
				return nil
			})

			for _, w := range tc.writes {
				n, err := writer.Write(w)
				assert.NoError(t, err)
				assert.Equal(t, len(w), n)
			}

			assert.NoError(t, writer.Close())
			assert.Equal(t, len(tc.expectEmits), len(emitted))

			for i, expected := range tc.expectEmits {
				assert.Equal(t, expected, string(emitted[i]))
			}
		})
	}
}
