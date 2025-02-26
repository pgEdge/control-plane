package ds_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

func TestSet(t *testing.T) {
	t.Run("Add", func(t *testing.T) {
		s := ds.Set[string]{}
		s.Add("foo")
		s.Add("bar")

		assert.True(t, s.Has("foo"))
		assert.True(t, s.Has("bar"))
		assert.False(t, s.Has("baz"))
		assert.Equal(t, 2, s.Size())
	})

	t.Run("Remove", func(t *testing.T) {
		s := ds.NewSet("foo")

		require.True(t, s.Has("foo"))

		s.Remove("foo")

		assert.False(t, s.Has("foo"))
		assert.Equal(t, 0, s.Size())
	})

	t.Run("Intersection", func(t *testing.T) {
		a := ds.NewSet("foo", "bar")
		b := ds.NewSet("foo", "baz")

		expected := ds.NewSet("foo")

		assert.Equal(t, expected, a.Intersection(b))
	})

	t.Run("Union", func(t *testing.T) {
		a := ds.NewSet("foo", "bar")
		b := ds.NewSet("foo", "baz")

		expected := ds.NewSet("foo", "bar", "baz")

		assert.Equal(t, expected, a.Union(b))
	})

	t.Run("Difference", func(t *testing.T) {
		a := ds.NewSet("foo", "bar")
		b := ds.NewSet("foo", "baz")

		expected := ds.NewSet("bar")

		assert.Equal(t, expected, a.Difference(b))
	})

	t.Run("SymmetricDifference", func(t *testing.T) {
		a := ds.NewSet("foo", "bar")
		b := ds.NewSet("foo", "baz")

		expected := ds.NewSet("bar", "baz")

		assert.Equal(t, expected, a.SymmetricDifference(b))
	})

	t.Run("ToSlice", func(t *testing.T) {
		s := ds.NewSet("foo", "bar")

		expected := []string{
			"foo",
			"bar",
		}

		assert.ElementsMatch(t, expected, s.ToSlice())
	})

	t.Run("Equal", func(t *testing.T) {
		a := ds.NewSet("foo", "bar")
		b := ds.NewSet("foo", "bar")
		c := ds.NewSet("foo", "baz")
		d := ds.NewSet[string]()

		assert.True(t, a.Equal(b))
		assert.True(t, b.Equal(a))
		assert.False(t, a.Equal(c))
		assert.False(t, c.Equal(a))
		assert.False(t, a.Equal(d))
		assert.False(t, d.Equal(a))
	})
}
