package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
)

func newTestServiceOrchestrator(t *testing.T) *Orchestrator {
	t.Helper()
	return &Orchestrator{
		serviceVersions: newTestServiceVersions(t, config.Config{
			DockerSwarm: config.DockerSwarm{
				ImageRepositoryHost: "registry.example.com/pgedge",
			},
		}),
	}
}

func serviceSpecWith(serviceType, version string, swarm *database.SwarmOpts) *database.ServiceInstanceSpec {
	svc := &database.ServiceSpec{
		ServiceType: serviceType,
		Version:     version,
	}
	if swarm != nil {
		svc.OrchestratorOpts = &database.OrchestratorOpts{Swarm: swarm}
	}
	return &database.ServiceInstanceSpec{ServiceSpec: svc}
}

func TestResolveServiceImage(t *testing.T) {
	o := newTestServiceOrchestrator(t)

	manifestImage, err := o.serviceVersions.GetServiceImage("mcp", "1.0.0")
	require.NoError(t, err)
	pinnedTag := manifestImage.Tag

	t.Run("Image override used directly, manifest not consulted", func(t *testing.T) {
		spec := serviceSpecWith("mcp", "1.0.0", &database.SwarmOpts{Image: "my-registry/mcp:dev"})

		img, err := o.resolveServiceImage(spec)
		require.NoError(t, err)
		assert.Equal(t, "my-registry/mcp:dev", img.Tag)
		// ResolvedImage must not be written when Image is set
		assert.Empty(t, spec.ServiceSpec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("Image override works for unknown version (bypasses manifest)", func(t *testing.T) {
		spec := serviceSpecWith("mcp", "unknown-version", &database.SwarmOpts{Image: "my-registry/mcp:dev"})

		img, err := o.resolveServiceImage(spec)
		require.NoError(t, err)
		assert.Equal(t, "my-registry/mcp:dev", img.Tag)
	})

	t.Run("Image takes precedence over ResolvedImage", func(t *testing.T) {
		spec := serviceSpecWith("mcp", "1.0.0", &database.SwarmOpts{
			Image:         "custom-override:latest",
			ResolvedImage: "previously-resolved:tag",
		})

		img, err := o.resolveServiceImage(spec)
		require.NoError(t, err)
		assert.Equal(t, "custom-override:latest", img.Tag)
		// ResolvedImage must not be touched when Image wins
		assert.Equal(t, "previously-resolved:tag", spec.ServiceSpec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("ResolvedImage used when Image is empty", func(t *testing.T) {
		spec := serviceSpecWith("mcp", "1.0.0", &database.SwarmOpts{ResolvedImage: "registry.example.com/pgedge:pinned"})

		img, err := o.resolveServiceImage(spec)
		require.NoError(t, err)
		assert.Equal(t, "registry.example.com/pgedge:pinned", img.Tag)
		// ResolvedImage must not be modified
		assert.Equal(t, "registry.example.com/pgedge:pinned", spec.ServiceSpec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("lazy backfill: resolves from manifest and writes ResolvedImage", func(t *testing.T) {
		spec := serviceSpecWith("mcp", "1.0.0", nil)

		img, err := o.resolveServiceImage(spec)
		require.NoError(t, err)
		assert.Equal(t, pinnedTag, img.Tag)
		require.NotNil(t, spec.ServiceSpec.OrchestratorOpts)
		require.NotNil(t, spec.ServiceSpec.OrchestratorOpts.Swarm)
		assert.Equal(t, pinnedTag, spec.ServiceSpec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("lazy backfill: unknown service type returns error", func(t *testing.T) {
		spec := serviceSpecWith("unknown-service", "latest", nil)

		_, err := o.resolveServiceImage(spec)
		assert.Error(t, err)
	})

	t.Run("lazy backfill: unknown version returns error", func(t *testing.T) {
		spec := serviceSpecWith("mcp", "99.99.99", nil)

		_, err := o.resolveServiceImage(spec)
		assert.Error(t, err)
	})
}

func TestReconcileServiceInstanceSpec(t *testing.T) {
	o := newTestServiceOrchestrator(t)

	manifestImage, err := o.serviceVersions.GetServiceImage("mcp", "1.0.0")
	require.NoError(t, err)
	pinnedTag := manifestImage.Tag

	t.Run("first creation: old nil, ResolvedImage written from manifest", func(t *testing.T) {
		spec := serviceSpecWith("mcp", "1.0.0", nil)
		require.NoError(t, o.ReconcileServiceInstanceSpec(nil, spec))
		require.NotNil(t, spec.ServiceSpec.OrchestratorOpts)
		require.NotNil(t, spec.ServiceSpec.OrchestratorOpts.Swarm)
		assert.Equal(t, pinnedTag, spec.ServiceSpec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("same version: old.ResolvedImage carried forward, manifest not re-consulted", func(t *testing.T) {
		old := serviceSpecWith("mcp", "1.0.0", &database.SwarmOpts{ResolvedImage: "registry.example.com/pgedge:pinned-mcp"})
		newSpec := serviceSpecWith("mcp", "1.0.0", nil)

		require.NoError(t, o.ReconcileServiceInstanceSpec(old, newSpec))
		require.NotNil(t, newSpec.ServiceSpec.OrchestratorOpts)
		require.NotNil(t, newSpec.ServiceSpec.OrchestratorOpts.Swarm)
		assert.Equal(t, "registry.example.com/pgedge:pinned-mcp", newSpec.ServiceSpec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("same version, old has no ResolvedImage: manifest lookup runs", func(t *testing.T) {
		old := serviceSpecWith("mcp", "1.0.0", nil)
		newSpec := serviceSpecWith("mcp", "1.0.0", nil)

		require.NoError(t, o.ReconcileServiceInstanceSpec(old, newSpec))
		require.NotNil(t, newSpec.ServiceSpec.OrchestratorOpts)
		assert.Equal(t, pinnedTag, newSpec.ServiceSpec.OrchestratorOpts.Swarm.ResolvedImage)
	})

	t.Run("version changed: stale ResolvedImage cleared, manifest re-consulted", func(t *testing.T) {
		postgrestImage, err := o.serviceVersions.GetServiceImage("postgrest", "14.5")
		require.NoError(t, err)

		old := serviceSpecWith("postgrest", "latest", &database.SwarmOpts{ResolvedImage: "registry.example.com/pgedge:postgrest-old"})
		newSpec := serviceSpecWith("postgrest", "14.5", &database.SwarmOpts{ResolvedImage: "registry.example.com/pgedge:postgrest-old"})

		require.NoError(t, o.ReconcileServiceInstanceSpec(old, newSpec))
		assert.Equal(t, postgrestImage.Tag, newSpec.ServiceSpec.OrchestratorOpts.Swarm.ResolvedImage)
		assert.NotEqual(t, "registry.example.com/pgedge:postgrest-old", newSpec.ServiceSpec.OrchestratorOpts.Swarm.ResolvedImage)
	})
}
