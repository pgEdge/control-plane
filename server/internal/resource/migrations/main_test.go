package migrations_test

import (
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/testutils"
	"github.com/stretchr/testify/require"
)

var update bool
var golden = &testutils.GoldenTest[*resource.State]{
	Compare: func(t testing.TB, expected, actual *resource.State) {
		// The json.RawValue ends up indented in our actual, so we'll round
		// trip the actual value to get the same indentation.
		raw, err := json.MarshalIndent(actual, "", "  ")
		require.NoError(t, err)

		var roundTrippedActual *resource.State
		require.NoError(t, json.Unmarshal(raw, &roundTrippedActual))

		require.Equal(t, expected, roundTrippedActual)
	},
}

func TestMain(m *testing.M) {
	flag.BoolVar(&update, "update", false, "update golden test outputs")
	flag.Parse()

	os.Exit(m.Run())
}
