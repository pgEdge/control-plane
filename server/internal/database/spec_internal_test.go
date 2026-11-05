package database

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

func TestVersionsFromImage(t *testing.T) {
	cases := []struct {
		name      string
		image     string
		wantPg    string
		wantSpock string
		wantOk    bool
	}{
		{"empty image", "", "", "", false},
		{"pgEdge tag", "127.0.0.1:5001/pgedge/pgedge-postgres:16.14-spock5.0.10-standard-1", "16.14", "5", true},
		{"pgEdge tag with digest", "ghcr.io/pgedge/pgedge-postgres:17.9-spock5.0.6-standard-2@sha256:abc123", "17.9", "5", true},
		{"unrecognized dev tag", "127.0.0.1:5001/pgedge/pgedge-postgres:my-dev-build", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pg, spock, ok := versionsFromImage(tc.image)
			assert.Equal(t, tc.wantOk, ok)
			if tc.wantOk {
				assert.Equal(t, tc.wantPg, pg)
				assert.Equal(t, tc.wantSpock, spock)
			}
		})
	}
}

func TestOrchestratorOptsSwarmImage(t *testing.T) {
	assert.Equal(t, "", (*OrchestratorOpts)(nil).swarmImage())
	assert.Equal(t, "", (&OrchestratorOpts{}).swarmImage())
	assert.Equal(t, "my-image", (&OrchestratorOpts{Swarm: &SwarmOpts{Image: "my-image"}}).swarmImage())
}

func TestValidateLocaleVersions(t *testing.T) {
	builtin := &Locale{Provider: "builtin", Locale: "C.UTF-8"}

	for _, tc := range []struct {
		name        string
		spec        *Spec
		expectedErr string
	}{
		{
			name: "no locale on postgres 16",
			spec: &Spec{PostgresVersion: "16.10", Nodes: []*Node{{Name: "n1"}}},
		},
		{
			name: "icu on postgres 16",
			spec: &Spec{
				PostgresVersion: "16.10",
				Locale:          &Locale{Provider: "icu", ICULocale: "de-DE"},
				Nodes:           []*Node{{Name: "n1"}},
			},
		},
		{
			name: "builtin on postgres 17",
			spec: &Spec{PostgresVersion: "17.6", Locale: builtin, Nodes: []*Node{{Name: "n1"}}},
		},
		{
			name:        "builtin on postgres 16",
			spec:        &Spec{PostgresVersion: "16.10", Locale: builtin, Nodes: []*Node{{Name: "n1"}}},
			expectedErr: "the 'builtin' locale provider requires Postgres 17 or later, got 16.10",
		},
		{
			// e.g. a node version that was defaulted from a node-level image
			// override by PopulateSpecDefaults.
			name: "builtin with a postgres 16 node",
			spec: &Spec{
				PostgresVersion: "18.1",
				Locale:          builtin,
				Nodes: []*Node{
					{Name: "n1"},
					{Name: "n2", PostgresVersion: "16.10"},
				},
			},
			expectedErr: "the 'builtin' locale provider requires Postgres 17 or later, got 16.10",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateLocaleVersions(tc.spec)
			if tc.expectedErr != "" {
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateServiceHosts(t *testing.T) {
	hostsByID := map[string]*host.Host{
		"host-1": {
			ID:                     "host-1",
			SupportedServices:      map[string][]string{"postgrest": {"14.5"}},
			DefaultServiceVersions: map[string]string{"postgrest": "14.5"},
		},
		"host-2": {
			ID:                     "host-2",
			SupportedServices:      map[string][]string{"postgrest": {"14.10"}},
			DefaultServiceVersions: map[string]string{"postgrest": "14.10"},
		},
		"host-3": {
			ID:                     "host-3",
			SupportedServices:      map[string][]string{},
			DefaultServiceVersions: map[string]string{},
		},
		// A host record from a Control Plane version that didn't report its
		// supported services.
		"host-4": {ID: "host-4"},
		"host-systemd": {
			ID:                     "host-systemd",
			Orchestrator:           config.OrchestratorSystemD,
			SupportedServices:      map[string][]string{"postgrest": {"14.5"}},
			DefaultServiceVersions: map[string]string{"postgrest": "14.5"},
		},
	}
	postgrest := func(version string, hostIDs ...string) *ServiceSpec {
		return &ServiceSpec{
			ServiceID:   "api",
			ServiceType: "postgrest",
			Version:     version,
			HostIDs:     hostIDs,
		}
	}

	for _, tc := range []struct {
		name        string
		service     *ServiceSpec
		expectedErr string
	}{
		{
			name:    "supported version",
			service: postgrest("14.5", "host-1"),
		},
		{
			name:    "latest",
			service: postgrest("latest", "host-1"),
		},
		{
			name:        "unsupported version",
			service:     postgrest("14.10", "host-1"),
			expectedErr: `service "api": host host-1 does not support postgrest version 14.10`,
		},
		{
			name:        "service not installed",
			service:     postgrest("14.5", "host-3"),
			expectedErr: `service "api": host host-3 does not support postgrest version 14.5`,
		},
		{
			name:        "latest with no common default",
			service:     postgrest("latest", "host-1", "host-2"),
			expectedErr: `service "api": unable to resolve version latest: no common default version of service type "postgrest" found between the given hosts`,
		},
		{
			name:    "host without supported services is not validated",
			service: postgrest("99.0", "host-4"),
		},
		{
			name: "systemd host with port",
			service: func() *ServiceSpec {
				svc := postgrest("14.5", "host-systemd")
				svc.Port = utils.PointerTo(0)
				return svc
			}(),
		},
		{
			name:        "systemd host without port",
			service:     postgrest("14.5", "host-systemd"),
			expectedErr: `service "api": port is required for host host-systemd, which uses the systemd orchestrator. set it to 0 to assign a random port`,
		},
		{
			name:        "unknown host",
			service:     postgrest("14.5", "host-5"),
			expectedErr: `service "api": host host-5 not found`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateServiceHosts([]*ServiceSpec{tc.service}, hostsByID)
			if tc.expectedErr != "" {
				assert.EqualError(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
