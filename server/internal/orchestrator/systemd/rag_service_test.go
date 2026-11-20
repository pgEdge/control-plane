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
	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

func TestGenerateRAGInstanceResources(t *testing.T) {
	const ragPath = "/usr/pgedge/rag-server2/bin/pgedge-rag-server"
	const instanceID = "storefront-rag-host-1"

	newOrchestrator := func(t *testing.T, installedVersion string) *Orchestrator {
		t.Helper()

		fsys := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fsys, ragPath, nil, 0o755))
		probe, err := NewInstalledVersionsProbe(
			func(_ context.Context, stdout, _ io.Writer, name string, args ...string) error {
				require.Equal(t, ragPath, name)
				require.Equal(t, []string{"--version"}, args)
				_, err := stdout.Write([]byte("pgEdge RAG Server\n  Version:    " + installedVersion + "\n"))
				return err
			},
			fsys,
			config.SystemD{OSFamily: config.OSFamilyDebian, RAGPath: ragPath},
		)
		require.NoError(t, err)

		return &Orchestrator{
			cfg: config.Config{
				DataDir: "/var/lib/pgedge-control-plane",
				SystemD: config.SystemD{RAGPath: ragPath},
			},
			versionsProbe: probe,
			userLookup: func(username string) (*user.User, error) {
				require.Equal(t, "pgedge", username)
				return &user.User{Uid: "999", Gid: "998", Username: username}, nil
			},
		}
	}
	ragConfig := func() map[string]any {
		return map[string]any{
			"pipelines": []any{
				map[string]any{
					"name": "default",
					"tables": []any{
						map[string]any{"table": "docs", "text_column": "content", "vector_column": "embedding"},
					},
					"embedding_llm": map[string]any{"provider": "openai", "model": "text-embedding-3-small", "api_key": "sk-embed"},
					"rag_llm":       map[string]any{"provider": "anthropic", "model": "claude-sonnet-4-5", "api_key": "sk-ant"},
				},
			},
		}
	}
	newSpec := func(version string, port *int) *database.ServiceInstanceSpec {
		return &database.ServiceInstanceSpec{
			ServiceInstanceID: instanceID,
			ServiceSpec: &database.ServiceSpec{
				ServiceID:   "rag",
				ServiceType: "rag",
				Version:     version,
				HostIDs:     []string{"host-1"},
				Config:      ragConfig(),
				ConnectAs:   "rag_user",
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
			ConnectAsUsername: "rag_user",
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
		o := newOrchestrator(t, "2.0.1")

		out, err := o.GenerateServiceInstanceResources(newSpec("latest", utils.PointerTo(3300)))
		require.NoError(t, err)
		resources := byID(t, out)

		unitName := "rag-" + instanceID + ".service"
		for _, id := range []resource.Identifier{
			filesystem.DirResourceIdentifier(instanceID + "-config"),
			common.RAGServiceKeysResourceIdentifier(instanceID),
			common.RAGPreflightResourceIdentifier(instanceID),
			RAGConfigIdentifier(instanceID),
			UnitResourceIdentifier(unitName, "storefront", "host-1"),
			ServiceInstanceResourceIdentifier(instanceID),
		} {
			assert.Contains(t, resources, id)
		}
		assert.Len(t, resources, 6)

		configDir, err := resource.ToResource[*filesystem.DirResource](resources[filesystem.DirResourceIdentifier(instanceID+"-config")])
		require.NoError(t, err)
		assert.Equal(t, "/etc/pgedge-rag/"+instanceID, configDir.Path)
		assert.Equal(t, 999, configDir.OwnerUID)
		assert.Equal(t, 998, configDir.OwnerGID)

		keys, err := resource.ToResource[*common.RAGServiceKeysResource](resources[common.RAGServiceKeysResourceIdentifier(instanceID)])
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"default_embedding.key": "sk-embed", "default_rag.key": "sk-ant"}, keys.Keys)
		assert.Equal(t, configDir.ID, keys.ParentID)
		assert.Equal(t, 999, keys.OwnerUID)
		assert.Equal(t, 998, keys.OwnerGID)

		preflight, err := resource.ToResource[*common.RAGPreflightResource](resources[common.RAGPreflightResourceIdentifier(instanceID)])
		require.NoError(t, err)
		assert.Equal(t, "n1", preflight.NodeName)
		assert.Equal(t, "rag_user", preflight.ConnectAsUsername)

		cfg, err := resource.ToResource[*RAGConfig](resources[RAGConfigIdentifier(instanceID)])
		require.NoError(t, err)
		assert.Equal(t, "storefront-n1-aaaa", cfg.DatabaseInstanceID, "connects to the first database host")
		assert.Contains(t, cfg.Dependencies(), database.InstanceResourceIdentifier("storefront-n1-aaaa"))
		assert.Contains(t, cfg.Dependencies(), common.RAGServiceKeysResourceIdentifier(instanceID))
		assert.Contains(t, cfg.Dependencies(), common.RAGPreflightResourceIdentifier(instanceID))
		assert.Equal(t, "/etc/pgedge-rag/"+instanceID+"/keys", cfg.Base.KeysDir)
		assert.Equal(t, "0.0.0.0", cfg.Base.ListenAddress)
		assert.Equal(t, 3300, cfg.Base.Port)
		assert.Equal(t, 999, cfg.Base.OwnerUID)
		assert.Equal(t, 998, cfg.Base.OwnerGID)
		assert.Equal(t, []string{"/base/database_host", "/base/database_port"}, cfg.DiffIgnore())

		unitRes, err := resource.ToResource[*UnitResource](resources[UnitResourceIdentifier(unitName, "storefront", "host-1")])
		require.NoError(t, err)
		assert.True(t, unitRes.RestartOnChange)
		assert.Equal(t, map[string]string{
			"ServiceType":       "rag",
			"ServiceInstanceID": instanceID,
			"Port":              "3300",
			"Version":           "2.0.1",
		}, UnitMetadata(unitRes.Options))
		assert.ElementsMatch(t, []resource.Identifier{
			configDir.Identifier(),
			keys.Identifier(),
			cfg.Identifier(),
		}, unitRes.ExtraDependencies)
		var execStart string
		for _, opt := range unitRes.Options {
			assert.NotEqual(t, "ExecReload", opt.Name, "RAG units are restarted, not reloaded")
			if opt.Name == "ExecStart" {
				execStart = opt.Value
			}
		}
		assert.Equal(t, ragPath+" -config /etc/pgedge-rag/"+instanceID+"/pgedge-rag-server.yaml", execStart)

		assert.Equal(t, utils.PointerTo(ServiceInstanceResourceIdentifier(instanceID)), out.MonitorDependency)
	})

	t.Run("pinned installed version", func(t *testing.T) {
		o := newOrchestrator(t, "2.0.1")

		_, err := o.GenerateServiceInstanceResources(newSpec("2.0.1", utils.PointerTo(3300)))
		assert.NoError(t, err)
	})

	t.Run("requested version not installed", func(t *testing.T) {
		o := newOrchestrator(t, "2.0.1")

		_, err := o.GenerateServiceInstanceResources(newSpec("2.0.0", utils.PointerTo(3300)))
		assert.EqualError(t, err, "rag version 2.0.1 is installed on this host, but version 2.0.0 was requested")
	})

	t.Run("missing port", func(t *testing.T) {
		o := newOrchestrator(t, "2.0.1")

		_, err := o.GenerateServiceInstanceResources(newSpec("2.0.1", nil))
		assert.EqualError(t, err, "port is required for systemd service instances, missing for service instance '"+instanceID+"'")
	})
}
