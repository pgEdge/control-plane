package activities_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

// A scheduled switchover that's delayed past its deadline must not be sent to
// Patroni.
func TestPerformSwitchoverPastDeadline(t *testing.T) {
	a := &activities.Activities{}
	deadline := time.Now().Add(-time.Minute)

	_, err := a.PerformSwitchover(context.Background(), &activities.PerformSwitchoverInput{
		DatabaseID:       "db",
		LeaderInstanceID: "n1-a",
		Deadline:         deadline,
	})
	require.Error(t, err)
	assert.EqualError(t, err, "switchover not performed: it had to start by "+deadline.Format(time.RFC3339))
}
