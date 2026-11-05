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
				ds.MustParsePgEdgeVersion("17.6", "5"),
				ds.MustParsePgEdgeVersion("17.6", "5"),
				ds.MustParsePgEdgeVersion("17.6", "5"),
			},
			supportedVersions: [][]*ds.PgEdgeVersion{
				{
					ds.MustParsePgEdgeVersion("16.10", "5"),
					ds.MustParsePgEdgeVersion("18.1", "5"),
					ds.MustParsePgEdgeVersion("17.6", "5"),
				},
				{
					ds.MustParsePgEdgeVersion("16.10", "5"),
					ds.MustParsePgEdgeVersion("18.1", "5"),
					ds.MustParsePgEdgeVersion("17.6", "5"),
				},
				{
					ds.MustParsePgEdgeVersion("16.10", "5"),
					ds.MustParsePgEdgeVersion("18.1", "5"),
					ds.MustParsePgEdgeVersion("17.6", "5"),
				},
			},
			expected: ds.MustParsePgEdgeVersion("17.6", "5"),
		},
		{
			name: "one newer",
			defaultVersions: []*ds.PgEdgeVersion{
				ds.MustParsePgEdgeVersion("17.7", "5"),
				ds.MustParsePgEdgeVersion("17.6", "5"),
				ds.MustParsePgEdgeVersion("17.6", "5"),
			},
			supportedVersions: [][]*ds.PgEdgeVersion{
				{
					ds.MustParsePgEdgeVersion("16.10", "5"),
					ds.MustParsePgEdgeVersion("18.1", "5"),
					ds.MustParsePgEdgeVersion("17.6", "5"),
					ds.MustParsePgEdgeVersion("17.7", "5"),
				},
				{
					ds.MustParsePgEdgeVersion("16.10", "5"),
					ds.MustParsePgEdgeVersion("18.1", "5"),
					ds.MustParsePgEdgeVersion("17.6", "5"),
				},
				{
					ds.MustParsePgEdgeVersion("16.10", "5"),
					ds.MustParsePgEdgeVersion("18.1", "5"),
					ds.MustParsePgEdgeVersion("17.6", "5"),
				},
			},
			expected: ds.MustParsePgEdgeVersion("17.6", "5"),
		},
		{
			name: "no overlaps",
			defaultVersions: []*ds.PgEdgeVersion{
				ds.MustParsePgEdgeVersion("18.0", "6"),
				ds.MustParsePgEdgeVersion("17.6", "5"),
				ds.MustParsePgEdgeVersion("17.6", "5"),
			},
			supportedVersions: [][]*ds.PgEdgeVersion{
				{
					ds.MustParsePgEdgeVersion("16.11", "6"),
					ds.MustParsePgEdgeVersion("17.7", "6"),
					ds.MustParsePgEdgeVersion("18.0", "6"),
				},
				{
					ds.MustParsePgEdgeVersion("16.10", "5"),
					ds.MustParsePgEdgeVersion("18.1", "5"),
					ds.MustParsePgEdgeVersion("17.6", "5"),
				},
				{
					ds.MustParsePgEdgeVersion("16.10", "5"),
					ds.MustParsePgEdgeVersion("18.1", "5"),
					ds.MustParsePgEdgeVersion("17.6", "5"),
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
				ds.MustParsePgEdgeVersion("18.0", "6"),
				ds.MustParsePgEdgeVersion("18.1", "5"),
				ds.MustParsePgEdgeVersion("18.1", "5"),
			},
			supportedVersions: [][]*ds.PgEdgeVersion{
				{
					ds.MustParsePgEdgeVersion("16.11", "5"),
					ds.MustParsePgEdgeVersion("17.6", "5"),
					ds.MustParsePgEdgeVersion("18.0", "5"),
				},
				{
					ds.MustParsePgEdgeVersion("16.10", "5"),
					ds.MustParsePgEdgeVersion("18.1", "5"),
					ds.MustParsePgEdgeVersion("17.6", "5"),
				},
				{
					ds.MustParsePgEdgeVersion("16.10", "5"),
					ds.MustParsePgEdgeVersion("18.1", "5"),
					ds.MustParsePgEdgeVersion("17.6", "5"),
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

func TestSupportsService(t *testing.T) {
	h := &host.Host{
		ID: "host-1",
		SupportedServices: map[string][]string{
			"postgrest": {"14.5", "14.10"},
		},
		DefaultServiceVersions: map[string]string{
			"postgrest": "14.5",
		},
	}

	for _, tc := range []struct {
		name        string
		serviceType string
		version     string
		expected    bool
	}{
		{name: "supported version", serviceType: "postgrest", version: "14.10", expected: true},
		{name: "unsupported version", serviceType: "postgrest", version: "13.0", expected: false},
		{name: "versions must match exactly", serviceType: "postgrest", version: "14.5.0", expected: false},
		{name: "latest with a default", serviceType: "postgrest", version: "latest", expected: true},
		{name: "unsupported service type", serviceType: "mcp", version: "1.0.0", expected: false},
		{name: "latest for unsupported service type", serviceType: "mcp", version: "latest", expected: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, h.SupportsService(tc.serviceType, tc.version))
		})
	}
}

func TestGreatestCommonDefaultServiceVersion(t *testing.T) {
	newHost := func(id, def string, supported ...string) *host.Host {
		return &host.Host{
			ID:                     id,
			SupportedServices:      map[string][]string{"postgrest": supported},
			DefaultServiceVersions: map[string]string{"postgrest": def},
		}
	}

	for _, tc := range []struct {
		name        string
		hosts       []*host.Host
		expected    string
		expectedErr string
	}{
		{
			name: "same defaults",
			hosts: []*host.Host{
				newHost("host-1", "14.5", "14.5"),
				newHost("host-2", "14.5", "14.5"),
			},
			expected: "14.5",
		},
		{
			name: "one newer default supported by all hosts",
			hosts: []*host.Host{
				newHost("host-1", "14.10", "14.5", "14.10"),
				newHost("host-2", "14.5", "14.5", "14.10"),
			},
			expected: "14.10",
		},
		{
			name: "compares versions numerically",
			hosts: []*host.Host{
				newHost("host-1", "14.10", "14.9", "14.10"),
				newHost("host-2", "14.9", "14.9", "14.10"),
			},
			expected: "14.10",
		},
		{
			name: "newer default unsupported by another host",
			hosts: []*host.Host{
				newHost("host-1", "14.10", "14.5", "14.10"),
				newHost("host-2", "14.5", "14.5"),
			},
			expected: "14.5",
		},
		{
			name: "no common default",
			hosts: []*host.Host{
				newHost("host-1", "14.10", "14.10"),
				newHost("host-2", "14.5", "14.5"),
			},
			expectedErr: `no common default version of service type "postgrest" found between the given hosts`,
		},
		{
			name: "host without the service",
			hosts: []*host.Host{
				newHost("host-1", "14.5", "14.5"),
				{ID: "host-2"},
			},
			expectedErr: `host 'host-2' does not support service type "postgrest"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			version, err := host.GreatestCommonDefaultServiceVersion("postgrest", tc.hosts...)
			if tc.expectedErr != "" {
				assert.EqualError(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, version)
			}
		})
	}
}
