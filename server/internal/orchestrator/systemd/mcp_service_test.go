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
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

func TestGenerateMCPInstanceResources(t *testing.T) {
	const mcpPath = "/usr/bin/pgedge-postgres-mcp"

	newOrchestrator := func(t *testing.T, installedVersion string) *Orchestrator {
		t.Helper()

		fsys := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fsys, mcpPath, nil, 0o755))
		probe, err := NewInstalledVersionsProbe(
			func(_ context.Context, stdout, _ io.Writer, name string, args ...string) error {
				require.Equal(t, "rpm", name)
				_, err := stdout.Write([]byte(installedVersion))
				return err
			},
			fsys,
			config.SystemD{OSFamily: config.OSFamilyRedHat, MCPPath: mcpPath},
		)
		require.NoError(t, err)

		return &Orchestrator{
			cfg: config.Config{
				DataDir: "/var/lib/pgedge-control-plane",
				SystemD: config.SystemD{MCPPath: mcpPath},
			},
			versionsProbe: probe,
			userLookup: func(username string) (*user.User, error) {
				require.Equal(t, "pgedge", username)
				return &user.User{Uid: "999", Gid: "998", Username: username}, nil
			},
		}
	}
	newSpec := func(version string, port *int, cfg map[string]any) *database.ServiceInstanceSpec {
		if cfg == nil {
			cfg = map[string]any{}
		}
		return &database.ServiceInstanceSpec{
			ServiceInstanceID: "storefront-mcp-server-host-1",
			ServiceSpec: &database.ServiceSpec{
				ServiceID:   "mcp-server",
				ServiceType: "mcp",
				Version:     version,
				HostIDs:     []string{"host-1"},
				Config:      cfg,
				ConnectAs:   "app",
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
					NodeName:  "n1",
					Instances: []*database.InstanceSpec{{InstanceID: "storefront-n1-aaaa", HostID: "host-1", Port: utils.PointerTo(6432)}},
				},
				{
					NodeName:  "n2",
					Instances: []*database.InstanceSpec{{InstanceID: "storefront-n2-bbbb", HostID: "host-2", Port: utils.PointerTo(6433)}},
				},
			},
			ConnectAsUsername: "app",
			ConnectAsPassword: "pass",
		}
	}
	byID := func(t *testing.T, out *database.ServiceInstanceResources) map[resource.Identifier]*resource.ResourceData {
		t.Helper()
		m := map[resource.Identifier]*resource.ResourceData{}
		for _, d := range out.Resources {
			m[d.Identifier] = d
		}
		return m
	}

	t.Run("generates resources", func(t *testing.T) {
		o := newOrchestrator(t, "1.1.0")

		out, err := o.GenerateServiceInstanceResources(newSpec("latest", utils.PointerTo(3200), nil))
		require.NoError(t, err)
		resources := byID(t, out)

		unitName := "mcp-storefront-mcp-server-host-1.service"
		for _, id := range []resource.Identifier{
			filesystem.DirResourceIdentifier("storefront-mcp-server-host-1-config"),
			filesystem.DirResourceIdentifier("storefront-mcp-server-host-1-data"),
			MCPConfigIdentifier("storefront-mcp-server-host-1"),
			UnitResourceIdentifier(unitName, "storefront", "host-1"),
			ServiceInstanceResourceIdentifier("storefront-mcp-server-host-1"),
		} {
			assert.Contains(t, resources, id)
		}

		cfg, err := resource.ToResource[*MCPConfig](resources[MCPConfigIdentifier("storefront-mcp-server-host-1")])
		require.NoError(t, err)
		assert.Equal(t, []string{"storefront-n1-aaaa", "storefront-n2-bbbb"}, cfg.DatabaseInstances)
		assert.Contains(t, cfg.Dependencies(), database.InstanceResourceIdentifier("storefront-n2-bbbb"))
		assert.Equal(t, 999, cfg.Base.OwnerUID)
		assert.Equal(t, 998, cfg.Base.OwnerGID)
		assert.Equal(t, ":3200", cfg.Base.Paths.HTTPAddress)
		assert.Equal(t, "/var/lib/pgedge/postgres-mcp/storefront-mcp-server-host-1", cfg.Base.Paths.DataDir)
		assert.Equal(t, "/var/lib/pgedge/postgres-mcp/storefront-mcp-server-host-1/tokens.yaml", cfg.Base.Paths.TokenFile)
		assert.Empty(t, cfg.Base.KBHostPath)

		configDir, err := resource.ToResource[*filesystem.DirResource](resources[filesystem.DirResourceIdentifier("storefront-mcp-server-host-1-config")])
		require.NoError(t, err)
		assert.Equal(t, "/etc/pgedge-mcp/storefront-mcp-server-host-1", configDir.Path)

		unitRes, err := resource.ToResource[*UnitResource](resources[UnitResourceIdentifier(unitName, "storefront", "host-1")])
		require.NoError(t, err)
		assert.True(t, unitRes.RestartOnChange)
		assert.Equal(t, map[string]string{
			"ServiceType":       "mcp",
			"ServiceInstanceID": "storefront-mcp-server-host-1",
			"Port":              "3200",
			"Version":           "1.1.0",
		}, UnitMetadata(unitRes.Options))

		assert.Equal(t, utils.PointerTo(ServiceInstanceResourceIdentifier("storefront-mcp-server-host-1")), out.MonitorDependency)
	})

	t.Run("knowledgebase paths", func(t *testing.T) {
		o := newOrchestrator(t, "1.1.0")
		kb := map[string]any{
			"kb_enabled":            true,
			"kb_embedding_provider": "voyage",
			"kb_embedding_model":    "voyage-3",
			"kb_embedding_api_key":  "kb-key",
		}

		out, err := o.GenerateServiceInstanceResources(newSpec("1.1.0", utils.PointerTo(3200), kb))
		require.NoError(t, err)
		cfg, err := resource.ToResource[*MCPConfig](byID(t, out)[MCPConfigIdentifier("storefront-mcp-server-host-1")])
		require.NoError(t, err)
		assert.Equal(t, "/var/lib/pgedge-control-plane/kb/nla-kb.db", cfg.Base.KBHostPath, "default KB path")
		assert.Equal(t, cfg.Base.KBHostPath, cfg.Base.Paths.KBDatabasePath, "the server reads the host path directly")

		kb["kb_database_host_path"] = "/srv/kb/custom.db"
		out, err = o.GenerateServiceInstanceResources(newSpec("1.1.0", utils.PointerTo(3200), kb))
		require.NoError(t, err)
		cfg, err = resource.ToResource[*MCPConfig](byID(t, out)[MCPConfigIdentifier("storefront-mcp-server-host-1")])
		require.NoError(t, err)
		assert.Equal(t, "/srv/kb/custom.db", cfg.Base.KBHostPath)
		assert.Equal(t, "/srv/kb/custom.db", cfg.Base.Paths.KBDatabasePath)
	})

	t.Run("requested version not installed", func(t *testing.T) {
		o := newOrchestrator(t, "1.1.0")

		_, err := o.GenerateServiceInstanceResources(newSpec("1.0.0", utils.PointerTo(3200), nil))
		assert.EqualError(t, err, "mcp version 1.1.0 is installed on this host, but version 1.0.0 was requested")
	})

	t.Run("missing port", func(t *testing.T) {
		o := newOrchestrator(t, "1.1.0")

		_, err := o.GenerateServiceInstanceResources(newSpec("1.1.0", nil, nil))
		assert.EqualError(t, err, "port is required for systemd service instances, missing for service instance 'storefront-mcp-server-host-1'")
	})
}
