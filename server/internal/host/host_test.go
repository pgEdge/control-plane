package host_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/host"
)

func TestGreatestCommonDefaultVersion(t *testing.T) {
	for _, tc := range []struct {
		name              string
		defaultVersions   []*ds.PgEdgeVersion
		supportedVersions [][]*ds.PgEdgeVersion
		expected          *ds.PgEdgeVersion
		expectedErr       string
	}{
		{
			name: "same supported versions",
			defaultVersions: []*ds.PgEdgeVersion{
				ds.MustPgEdgeVersion("17.6", "5"),
				ds.MustPgEdgeVersion("17.6", "5"),
				ds.MustPgEdgeVersion("17.6", "5"),
			},
			supportedVersions: [][]*ds.PgEdgeVersion{
				{
					ds.MustPgEdgeVersion("16.10", "5"),
					ds.MustPgEdgeVersion("18.1", "5"),
					ds.MustPgEdgeVersion("17.6", "5"),
				},
				{
					ds.MustPgEdgeVersion("16.10", "5"),
					ds.MustPgEdgeVersion("18.1", "5"),
					ds.MustPgEdgeVersion("17.6", "5"),
				},
				{
					ds.MustPgEdgeVersion("16.10", "5"),
					ds.MustPgEdgeVersion("18.1", "5"),
					ds.MustPgEdgeVersion("17.6", "5"),
				},
			},
			expected: ds.MustPgEdgeVersion("17.6", "5"),
		},
		{
			name: "one newer",
			defaultVersions: []*ds.PgEdgeVersion{
				ds.MustPgEdgeVersion("17.7", "5"),
				ds.MustPgEdgeVersion("17.6", "5"),
				ds.MustPgEdgeVersion("17.6", "5"),
			},
			supportedVersions: [][]*ds.PgEdgeVersion{
				{
					ds.MustPgEdgeVersion("16.10", "5"),
					ds.MustPgEdgeVersion("18.1", "5"),
					ds.MustPgEdgeVersion("17.6", "5"),
					ds.MustPgEdgeVersion("17.7", "5"),
				},
				{
					ds.MustPgEdgeVersion("16.10", "5"),
					ds.MustPgEdgeVersion("18.1", "5"),
					ds.MustPgEdgeVersion("17.6", "5"),
				},
				{
					ds.MustPgEdgeVersion("16.10", "5"),
					ds.MustPgEdgeVersion("18.1", "5"),
					ds.MustPgEdgeVersion("17.6", "5"),
				},
			},
			expected: ds.MustPgEdgeVersion("17.6", "5"),
		},
		{
			name: "no overlaps",
			defaultVersions: []*ds.PgEdgeVersion{
				ds.MustPgEdgeVersion("18.0", "6"),
				ds.MustPgEdgeVersion("17.6", "5"),
				ds.MustPgEdgeVersion("17.6", "5"),
			},
			supportedVersions: [][]*ds.PgEdgeVersion{
				{
					ds.MustPgEdgeVersion("16.11", "6"),
					ds.MustPgEdgeVersion("17.7", "6"),
					ds.MustPgEdgeVersion("18.0", "6"),
				},
				{
					ds.MustPgEdgeVersion("16.10", "5"),
					ds.MustPgEdgeVersion("18.1", "5"),
					ds.MustPgEdgeVersion("17.6", "5"),
				},
				{
					ds.MustPgEdgeVersion("16.10", "5"),
					ds.MustPgEdgeVersion("18.1", "5"),
					ds.MustPgEdgeVersion("17.6", "5"),
				},
			},
			expectedErr: "no common default versions found between the given hosts",
		},
		{
			// Differs from the above because they do have an overlapping
			// version. But, since this function is intended to find defaults we
			// still return an error. Unlike the above, this combination of
			// hosts is usable, but the user will need to set a specific
			// version.
			name: "no overlapping defaults",
			defaultVersions: []*ds.PgEdgeVersion{
				ds.MustPgEdgeVersion("18.0", "6"),
				ds.MustPgEdgeVersion("18.1", "5"),
				ds.MustPgEdgeVersion("18.1", "5"),
			},
			supportedVersions: [][]*ds.PgEdgeVersion{
				{
					ds.MustPgEdgeVersion("16.11", "5"),
					ds.MustPgEdgeVersion("17.6", "5"),
					ds.MustPgEdgeVersion("18.0", "5"),
				},
				{
					ds.MustPgEdgeVersion("16.10", "5"),
					ds.MustPgEdgeVersion("18.1", "5"),
					ds.MustPgEdgeVersion("17.6", "5"),
				},
				{
					ds.MustPgEdgeVersion("16.10", "5"),
					ds.MustPgEdgeVersion("18.1", "5"),
					ds.MustPgEdgeVersion("17.6", "5"),
				},
			},
			expectedErr: "no common default versions found between the given hosts",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hosts := make([]*host.Host, len(tc.defaultVersions))
			for i, v := range tc.defaultVersions {
				hosts[i] = &host.Host{
					DefaultPgEdgeVersion:    v,
					SupportedPgEdgeVersions: tc.supportedVersions[i],
				}
			}

			result, err := host.GreatestCommonDefaultVersion(hosts...)
			if tc.expectedErr != "" {
				assert.Nil(t, result)
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}
