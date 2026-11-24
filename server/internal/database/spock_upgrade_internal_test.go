package database

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

func TestCheckSpockUpgradeVersions(t *testing.T) {
	fresh := utils.PointerTo(time.Now())
	stale := utils.PointerTo(time.Now().Add(-10 * InstanceMonitorRefreshInterval))

	instance := func(id, spockVersion string, updatedAt *time.Time) *Instance {
		status := &InstanceStatus{StatusUpdatedAt: updatedAt}
		if spockVersion != "" {
			status.SpockVersion = utils.PointerTo(spockVersion)
		}
		return &Instance{InstanceID: id, Status: status}
	}

	for _, tc := range []struct {
		name        string
		instances   []*Instance
		expectedErr []string
	}{
		{
			name: "all instances at the minimum version",
			instances: []*Instance{
				instance("db-n1-a", "5.0.14", fresh),
				instance("db-n2-b", "5.0.14", fresh),
			},
		},
		{
			name: "instances newer than the minimum version",
			instances: []*Instance{
				instance("db-n1-a", "5.0.15", fresh),
				instance("db-n2-b", "6.0.0", fresh),
			},
		},
		{
			name: "an instance older than the minimum version",
			instances: []*Instance{
				instance("db-n1-a", "5.0.14", fresh),
				instance("db-n2-b", "5.0.13", fresh),
			},
			expectedErr: []string{"instance db-n2-b runs spock 5.0.13, but upgrading to the next spock major version requires 5.0.14 or later"},
		},
		{
			name: "an instance without a reported spock version",
			instances: []*Instance{
				instance("db-n1-a", "", fresh),
			},
			expectedErr: []string{"the spock version of instance db-n1-a is unknown"},
		},
		{
			name: "an instance with a stale status",
			instances: []*Instance{
				instance("db-n1-a", "5.0.14", stale),
			},
			expectedErr: []string{"the spock version of instance db-n1-a is unknown"},
		},
		{
			name:        "no instances",
			instances:   nil,
			expectedErr: []string{"the database has no instances"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := errors.Join(checkSpockUpgradeVersions(tc.instances)...)
			if len(tc.expectedErr) == 0 {
				assert.NoError(t, err)
				return
			}
			for _, expected := range tc.expectedErr {
				assert.ErrorContains(t, err, expected)
			}
		})
	}
}

func TestSpockUpgradeInProgress(t *testing.T) {
	instance := func(spockVersion string, status *InstanceStatus) *Instance {
		return &Instance{PgEdgeVersion: ds.MustParsePgEdgeVersion("17.12", spockVersion), Status: status}
	}
	stale := &InstanceStatus{StatusUpdatedAt: utils.PointerTo(time.Now().Add(-10 * InstanceMonitorRefreshInterval))}

	assert.True(t, spockUpgradeInProgress([]*Instance{instance("6", nil), instance("5", nil)}, "6"),
		"an instance still recorded on spock 5 means a retried upgrade")
	assert.True(t, spockUpgradeInProgress([]*Instance{instance("6", nil), instance("5", stale)}, "6"),
		"an instance that isn't reporting status still counts")
	assert.False(t, spockUpgradeInProgress([]*Instance{instance("6", nil), instance("6", stale)}, "6"),
		"every instance on the target major")
	assert.False(t, spockUpgradeInProgress([]*Instance{{}}, "6"),
		"instances without a recorded version are ignored")
}

func TestIsSpockMajorUpgrade(t *testing.T) {
	for _, tc := range []struct {
		current, updated string
		expected         bool
	}{
		{current: "5", updated: "6", expected: true},
		{current: "5.0.14", updated: "6", expected: true},
		{current: "5", updated: "5.0.14", expected: false},
		{current: "6", updated: "5", expected: false},
		{current: "", updated: "6", expected: false},
		{current: "5", updated: "", expected: false},
		{current: "five", updated: "6", expected: false},
	} {
		t.Run(tc.current+"->"+tc.updated, func(t *testing.T) {
			assert.Equal(t, tc.expected, IsSpockMajorUpgrade(tc.current, tc.updated))
		})
	}
}
