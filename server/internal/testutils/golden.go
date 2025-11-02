package testutils

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// GoldenTest is a helper for "golden tests," which compare an actual output to
// a known good output that's saved to a file and committed to the repository.
type GoldenTest[T any] struct {
	// FileExtension defaults to '.json'.
	FileExtension string
	// Marshal defaults to a wrapper around json.MarshalIndent
	Marshal func(v any) ([]byte, error)
	// Unmarshal defaults to json.Unmarshal
	Unmarshal func(data []byte, v any) error
	// Compare defaults to require.Compare
	Compare func(t testing.TB, expected, actual T)
}

func (g *GoldenTest[T]) path(t testing.TB) string {
	ext := ".json"
	if g.FileExtension != "" {
		ext = g.FileExtension
	}
	return filepath.Join("golden_test", t.Name()+ext)
}

func (g *GoldenTest[T]) update(t testing.TB, actual T) {
	marshal := func(v any) ([]byte, error) {
		return json.MarshalIndent(v, "", "  ")
	}
	if g.Marshal != nil {
		marshal = g.Marshal
	}

	data, err := marshal(actual)
	require.NoError(t, err)

	expectedPath := g.path(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(expectedPath), 0o755))
	require.NoError(t, os.WriteFile(g.path(t), data, 0o644))
}

func (g *GoldenTest[T]) Run(t testing.TB, actual T, update bool) {
	t.Helper()

	if update {
		g.update(t, actual)
	}

	expectedPath := g.path(t)
	data, err := os.ReadFile(expectedPath)
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("golden test output %s does not exist. re-run this test with update enabled to create it.", expectedPath)
	} else if err != nil {
		t.Fatalf("failed to stat golden test output %s: %s", expectedPath, err)
	}

	unmarshal := json.Unmarshal
	if g.Unmarshal != nil {
		unmarshal = g.Unmarshal
	}

	var expected T
	require.NoError(t, unmarshal(data, &expected))

	if g.Compare != nil {
		g.Compare(t, expected, actual)
	} else {
		require.Equal(t, expected, actual)
	}
}
