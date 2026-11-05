package systemd

import (
	"context"
	"io"
	"os/user"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

func TestGeneratePostgRESTInstanceResources(t *testing.T) {
	const postgrestPath = "/usr/bin/postgrest"

	newOrchestrator := func(t *testing.T, installedVersion string) *Orchestrator {
		t.Helper()

		fsys := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fsys, postgrestPath, nil, 0o755))
		probe, err := NewInstalledVersionsProbe(
			func(_ context.Context, stdout, _ io.Writer, name string, args ...string) error {
				_, err := stdout.Write([]byte("PostgREST " + installedVersion + "\n"))
				return err
			},
			fsys,
			config.SystemD{OSFamily: config.OSFamilyDebian, PostgRESTPath: postgrestPath},
		)
		require.NoError(t, err)

		return &Orchestrator{
			cfg: config.Config{
				SystemD: config.SystemD{PostgRESTPath: postgrestPath},
			},
			versionsProbe: probe,
			userLookup: func(username string) (*user.User, error) {
				return &user.User{Uid: "998", Gid: "997", Username: username}, nil
			},
		}
	}
	newSpec := func(version string, port *int) *database.ServiceInstanceSpec {
		return &database.ServiceInstanceSpec{
			ServiceInstanceID: "storefront-api-host-1",
			ServiceSpec: &database.ServiceSpec{
				ServiceID:   "api",
				ServiceType: "postgrest",
				Version:     version,
				HostIDs:     []string{"host-1"},
				Config: map[string]any{
					"db_anon_role": "web_anon",
				},
				ConnectAs: "app",
			},
			DatabaseID:   "storefront",
			DatabaseName: "storefront",
			HostID:       "host-1",
			NodeName:     "n1",
			DatabaseHosts: []database.ServiceHostEntry{
				{Host: "postgres-storefront-n1-aaaa", Port: 5432},
				{Host: "postgres-storefront-n2-bbbb", Port: 5432},
			},
			Port: port,
			DatabaseNodes: []*database.NodeInstances{
				{
					NodeName: "n1",
					Instances: []*database.InstanceSpec{
						{InstanceID: "storefront-n1-aaaa", HostID: "host-1", Port: utils.PointerTo(6432)},
					},
				},
				{
					NodeName: "n2",
					Instances: []*database.InstanceSpec{
						{InstanceID: "storefront-n2-bbbb", HostID: "host-2", Port: utils.PointerTo(6433)},
					},
				},
			},
			ConnectAsUsername: "app",
			ConnectAsPassword: "pass",
		}
	}

	t.Run("generates resources", func(t *testing.T) {
		o := newOrchestrator(t, "14.5")

		out, err := o.GenerateServiceInstanceResources(newSpec("latest", utils.PointerTo(3100)))
		require.NoError(t, err)

		byID := map[resource.Identifier]*resource.ResourceData{}
		for _, d := range out.Resources {
			byID[d.Identifier] = d
		}
		unitName := "postgrest-storefront-api-host-1.service"
		for _, id := range []resource.Identifier{
			common.PostgRESTPreflightResourceIdentifier("api"),
			common.PostgRESTAuthenticatorIdentifier("api", "n1"),
			common.PostgRESTAuthenticatorIdentifier("api", "n2"),
			PostgRESTConfigIdentifier("storefront-api-host-1"),
			UnitResourceIdentifier(unitName, "storefront", "host-1"),
			ServiceInstanceResourceIdentifier("storefront-api-host-1"),
		} {
			assert.Contains(t, byID, id)
		}

		config, err := resource.ToResource[*PostgRESTConfig](byID[PostgRESTConfigIdentifier("storefront-api-host-1")])
		require.NoError(t, err)
		assert.Equal(t, []string{"storefront-n1-aaaa", "storefront-n2-bbbb"}, config.DatabaseInstances)
		assert.Contains(t, config.Dependencies(), database.InstanceResourceIdentifier("storefront-n2-bbbb"))
		assert.Equal(t, 998, config.Base.OwnerUID)
		assert.Equal(t, 997, config.Base.OwnerGID)

		unitRes, err := resource.ToResource[*UnitResource](byID[UnitResourceIdentifier(unitName, "storefront", "host-1")])
		require.NoError(t, err)
		assert.True(t, unitRes.RestartOnChange)
		assert.Equal(t, map[string]string{
			"ServiceType":       "postgrest",
			"ServiceInstanceID": "storefront-api-host-1",
			"Port":              "3100",
			"Version":           "14.5",
		}, UnitMetadata(unitRes.Options))

		assert.Equal(t, database.ServiceInstanceStateCreating, out.ServiceInstance.State)
		assert.Equal(t, utils.PointerTo(ServiceInstanceResourceIdentifier("storefront-api-host-1")), out.MonitorDependency)
	})

	t.Run("requested version not installed", func(t *testing.T) {
		o := newOrchestrator(t, "14.5")

		_, err := o.GenerateServiceInstanceResources(newSpec("14.10", utils.PointerTo(3100)))
		assert.EqualError(t, err, "postgrest version 14.5 is installed on this host, but version 14.10 was requested")
	})

	t.Run("missing port", func(t *testing.T) {
		o := newOrchestrator(t, "14.5")

		_, err := o.GenerateServiceInstanceResources(newSpec("14.5", nil))
		assert.EqualError(t, err, "port is required for systemd service instances, missing for service instance 'storefront-api-host-1'")
	})

	t.Run("unsupported service type", func(t *testing.T) {
		o := newOrchestrator(t, "14.5")
		spec := newSpec("1.0.0", utils.PointerTo(3100))
		spec.ServiceSpec.ServiceType = "mcp"

		_, err := o.GenerateServiceInstanceResources(spec)
		assert.EqualError(t, err, `service type "mcp" is not supported on systemd hosts`)
	})
}
