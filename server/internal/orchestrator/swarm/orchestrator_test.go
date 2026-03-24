package swarm

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/zfs"
)

func TestServiceInstanceName(t *testing.T) {
	tests := []struct {
		name        string
		serviceType string
		databaseID  string
		serviceID   string
		hostID      string
	}{
		{
			name:        "short host ID",
			serviceType: "mcp",
			databaseID:  "my-db",
			serviceID:   "mcp-server",
			hostID:      "host1",
		},
		{
			name:        "UUID host ID",
			serviceType: "mcp",
			databaseID:  "my-db",
			serviceID:   "mcp-server",
			hostID:      "dbf5779c-492a-11f0-b11a-1b8cb15693a8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ServiceInstanceName(tt.serviceType, tt.databaseID, tt.serviceID, tt.hostID)

			// Verify format: {serviceType}-{databaseID}-{serviceID}-{8charHash}
			prefix := fmt.Sprintf("%s-%s-%s-", tt.serviceType, tt.databaseID, tt.serviceID)
			if !strings.HasPrefix(got, prefix) {
				t.Errorf("ServiceInstanceName() = %q, want prefix %q", got, prefix)
			}

			// Verify the suffix is exactly 8 characters (base36 hash)
			suffix := strings.TrimPrefix(got, prefix)
			if len(suffix) != 8 {
				t.Errorf("ServiceInstanceName() hash suffix = %q (len %d), want 8 chars", suffix, len(suffix))
			}

			// Verify deterministic
			got2 := ServiceInstanceName(tt.serviceType, tt.databaseID, tt.serviceID, tt.hostID)
			if got != got2 {
				t.Errorf("ServiceInstanceName() not deterministic: %q != %q", got, got2)
			}
		})
	}

	// Verify different host IDs produce different names
	t.Run("different hosts produce different names", func(t *testing.T) {
		name1 := ServiceInstanceName("mcp", "db1", "svc1", "host-a")
		name2 := ServiceInstanceName("mcp", "db1", "svc1", "host-b")
		if name1 == name2 {
			t.Errorf("different host IDs should produce different names, both got %q", name1)
		}
	})
}

// testOrchestrator builds a minimal Orchestrator for unit-testing
// instanceResources(). It does NOT require a running Docker daemon.
func testOrchestrator(zfsCfg config.ZFS) *Orchestrator {
	cfg := config.Config{
		DataDir:          "/var/lib/pgedge",
		DatabaseOwnerUID: 26,
		PeerAddresses:    []string{"10.0.0.1"},
		ClientAddresses:  []string{"10.0.0.1"},
		DockerSwarm: config.DockerSwarm{
			ImageRepositoryHost:        "ghcr.io/pgedge",
			DatabaseNetworksCIDR:       "10.128.128.0/18",
			DatabaseNetworksSubnetBits: 26,
		},
		ZFS: zfsCfg,
	}
	return &Orchestrator{
		cfg:      cfg,
		versions: NewVersions(cfg),
		logger:   zerolog.Nop(),
		dbNetworkAllocator: Allocator{
			Prefix: netip.MustParsePrefix("10.128.128.0/18"),
			Bits:   26,
		},
		bridgeNetwork: &docker.NetworkInfo{
			Name:    "bridge",
			ID:      "bridge-id",
			Subnet:  netip.MustParsePrefix("172.17.0.0/16"),
			Gateway: netip.MustParseAddr("172.17.0.1"),
		},
		cpus:        4,
		memBytes:    8589934592, // 8 GiB
		swarmNodeID: "test-swarm-node",
	}
}

// testInstanceSpec creates a minimal InstanceSpec suitable for
// instanceResources(). Set cloneConfig to non-nil to exercise the clone path.
func testInstanceSpec(cloneConfig *database.CloneConfig) *database.InstanceSpec {
	return &database.InstanceSpec{
		InstanceID:    "test-db-n1-abc12345",
		DatabaseID:    "test-db",
		HostID:        "host1",
		DatabaseName:  "mydb",
		NodeName:      "n1",
		NodeOrdinal:   1,
		PgEdgeVersion: host.MustPgEdgeVersion("18.2", "5"),
		ClusterSize:   1,
		CloneConfig:   cloneConfig,
	}
}

// hasResourceType returns true if the slice contains at least one resource
// with the given type.
func hasResourceType(resources []resource.Resource, typ resource.Type) bool {
	for _, r := range resources {
		if r.Identifier().Type == typ {
			return true
		}
	}
	return false
}

// findResource returns the first resource with the given identifier, or nil.
func findResource(resources []resource.Resource, id resource.Identifier) resource.Resource {
	for _, r := range resources {
		if r.Identifier() == id {
			return r
		}
	}
	return nil
}

func TestInstanceResources_ZFSClonePath(t *testing.T) {
	orch := testOrchestrator(config.ZFS{Enabled: true, Pool: "tank"})
	spec := testInstanceSpec(&database.CloneConfig{
		SourceDatabaseID: "source-db",
		SourceNodeName:   "n1",
	})

	instance, resources, err := orch.instanceResources(spec)
	require.NoError(t, err)

	// Should contain ZFS snapshot and clone resources
	assert.True(t, hasResourceType(resources, zfs.ResourceTypeSnapshot),
		"clone path should emit a ZFS snapshot resource")
	assert.True(t, hasResourceType(resources, zfs.ResourceTypeClone),
		"clone path should emit a ZFS clone resource")

	// Should contain SpockCleanup resource
	assert.True(t, hasResourceType(resources, zfs.ResourceTypeCleanup),
		"clone path should emit a SpockCleanup resource")

	// Should NOT contain a DirResource for the data path
	for _, r := range resources {
		if dir, ok := r.(*filesystem.DirResource); ok {
			assert.NotEqual(t, spec.InstanceID+"-data", dir.ID,
				"clone path should not emit a DirResource for the data directory")
		}
	}

	// Should NOT contain a ZFS dataset resource (dataset is for non-clone ZFS path)
	assert.False(t, hasResourceType(resources, zfs.ResourceTypeDataset),
		"clone path should not emit a ZFS dataset resource")

	// SpockCleanup should be in OrchestratorDependencies so the instance
	// resource waits for cleanup to finish.
	cleanupID := zfs.CleanupIdentifier(spec.InstanceID)
	assert.Contains(t, instance.OrchestratorDependencies, cleanupID,
		"instance OrchestratorDependencies should include SpockCleanup")

	// Verify the PostgresServiceSpecResource.DataDirDep points to the clone
	// resource (the last resource in the data directory chain).
	serviceSpec := findServiceSpec(resources)
	require.NotNil(t, serviceSpec, "should emit a PostgresServiceSpecResource")
	assert.Equal(t, zfs.CloneIdentifier(spec.InstanceID), serviceSpec.DataDirDep,
		"service spec DataDirDep should reference the Clone resource")

	// Verify the snapshot and clone reference the correct source instance ID.
	sourceInstanceID := database.InstanceIDFor(spec.HostID, "source-db", "n1")
	snap := findResource(resources, zfs.SnapshotIdentifier(spec.InstanceID))
	require.NotNil(t, snap)
	assert.Equal(t, sourceInstanceID, snap.(*zfs.Snapshot).SourceInstanceID)

	clone := findResource(resources, zfs.CloneIdentifier(spec.InstanceID))
	require.NotNil(t, clone)
	assert.Equal(t, sourceInstanceID, clone.(*zfs.Clone).SourceInstanceID)
	assert.Equal(t, "tank", clone.(*zfs.Clone).Pool)
}

func TestInstanceResources_ZFSNonClonePath(t *testing.T) {
	orch := testOrchestrator(config.ZFS{Enabled: true, Pool: "tank"})
	spec := testInstanceSpec(nil) // no CloneConfig

	instance, resources, err := orch.instanceResources(spec)
	require.NoError(t, err)

	// Should contain a ZFS dataset resource
	assert.True(t, hasResourceType(resources, zfs.ResourceTypeDataset),
		"ZFS non-clone path should emit a ZFS dataset resource")

	// Should NOT contain clone-specific resources
	assert.False(t, hasResourceType(resources, zfs.ResourceTypeSnapshot),
		"non-clone path should not emit a ZFS snapshot resource")
	assert.False(t, hasResourceType(resources, zfs.ResourceTypeClone),
		"non-clone path should not emit a ZFS clone resource")
	assert.False(t, hasResourceType(resources, zfs.ResourceTypeScrub),
		"non-clone path should not emit a ScrubReplicationState resource")
	assert.False(t, hasResourceType(resources, zfs.ResourceTypeCleanup),
		"non-clone path should not emit a SpockCleanup resource")

	// Should NOT contain a DirResource for the data path
	for _, r := range resources {
		if dir, ok := r.(*filesystem.DirResource); ok {
			assert.NotEqual(t, spec.InstanceID+"-data", dir.ID,
				"ZFS path should not emit a DirResource for the data directory")
		}
	}

	// Verify the PostgresServiceSpecResource.DataDirDep points to the dataset.
	serviceSpec := findServiceSpec(resources)
	require.NotNil(t, serviceSpec, "should emit a PostgresServiceSpecResource")
	assert.Equal(t, zfs.DatasetIdentifier(spec.InstanceID), serviceSpec.DataDirDep,
		"service spec DataDirDep should reference the ZFS dataset resource")

	// Verify the dataset has the correct pool and mount point.
	ds := findResource(resources, zfs.DatasetIdentifier(spec.InstanceID))
	require.NotNil(t, ds)
	dataset := ds.(*zfs.Dataset)
	assert.Equal(t, "tank", dataset.Pool)
	assert.Contains(t, dataset.MountPoint, spec.InstanceID)

	// OrchestratorDependencies should NOT include SpockCleanup.
	cleanupID := zfs.CleanupIdentifier(spec.InstanceID)
	assert.NotContains(t, instance.OrchestratorDependencies, cleanupID,
		"non-clone instance should not depend on SpockCleanup")
}

func TestInstanceResources_NonZFSPath(t *testing.T) {
	orch := testOrchestrator(config.ZFS{Enabled: false})
	spec := testInstanceSpec(nil) // no CloneConfig, no ZFS

	instance, resources, err := orch.instanceResources(spec)
	require.NoError(t, err)

	// Should contain a DirResource for the data directory
	var foundDataDir bool
	for _, r := range resources {
		if dir, ok := r.(*filesystem.DirResource); ok && dir.ID == spec.InstanceID+"-data" {
			foundDataDir = true
			assert.Equal(t, "data", dir.Path,
				"non-ZFS data dir should use relative path 'data'")
			break
		}
	}
	assert.True(t, foundDataDir, "non-ZFS path should emit a DirResource for the data directory")

	// Should NOT contain any ZFS resources
	assert.False(t, hasResourceType(resources, zfs.ResourceTypeDataset),
		"non-ZFS path should not emit a ZFS dataset resource")
	assert.False(t, hasResourceType(resources, zfs.ResourceTypeSnapshot),
		"non-ZFS path should not emit a ZFS snapshot resource")
	assert.False(t, hasResourceType(resources, zfs.ResourceTypeClone),
		"non-ZFS path should not emit a ZFS clone resource")
	assert.False(t, hasResourceType(resources, zfs.ResourceTypeScrub),
		"non-ZFS path should not emit a ScrubReplicationState resource")
	assert.False(t, hasResourceType(resources, zfs.ResourceTypeCleanup),
		"non-ZFS path should not emit a SpockCleanup resource")

	// Verify the PostgresServiceSpecResource.DataDirDep points to the DirResource.
	serviceSpec := findServiceSpec(resources)
	require.NotNil(t, serviceSpec, "should emit a PostgresServiceSpecResource")
	assert.Equal(t, filesystem.DirResourceIdentifier(spec.InstanceID+"-data"), serviceSpec.DataDirDep,
		"service spec DataDirDep should reference the filesystem DirResource")

	// OrchestratorDependencies should NOT include SpockCleanup.
	cleanupID := zfs.CleanupIdentifier(spec.InstanceID)
	assert.NotContains(t, instance.OrchestratorDependencies, cleanupID,
		"non-clone instance should not depend on SpockCleanup")
}

func TestInstanceResources_NonZFSWithCloneConfigStillEmitsDirResource(t *testing.T) {
	// When ZFS is disabled but CloneConfig is present (this shouldn't happen
	// in production due to validation, but we verify the orchestrator's
	// defensive behavior), the non-ZFS code path should be used and
	// SpockCleanup should still be emitted because CloneConfig is set.
	orch := testOrchestrator(config.ZFS{Enabled: false})
	spec := testInstanceSpec(&database.CloneConfig{
		SourceDatabaseID: "source-db",
		SourceNodeName:   "n1",
	})

	instance, resources, err := orch.instanceResources(spec)
	require.NoError(t, err)

	// Data dir should be a DirResource (ZFS is off).
	var foundDataDir bool
	for _, r := range resources {
		if dir, ok := r.(*filesystem.DirResource); ok && dir.ID == spec.InstanceID+"-data" {
			foundDataDir = true
			break
		}
	}
	assert.True(t, foundDataDir,
		"non-ZFS path should emit a DirResource even when CloneConfig is set")

	// SpockCleanup should still be emitted since CloneConfig is set.
	assert.True(t, hasResourceType(resources, zfs.ResourceTypeCleanup),
		"CloneConfig set should cause SpockCleanup to be emitted regardless of ZFS setting")

	// SpockCleanup should be in OrchestratorDependencies.
	cleanupID := zfs.CleanupIdentifier(spec.InstanceID)
	assert.Contains(t, instance.OrchestratorDependencies, cleanupID,
		"instance OrchestratorDependencies should include SpockCleanup when CloneConfig is set")
}

func TestInstanceResources_DependencyChainIntegrity(t *testing.T) {
	tests := []struct {
		name           string
		zfsCfg         config.ZFS
		cloneConfig    *database.CloneConfig
		expectedDepType resource.Type
	}{
		{
			name:            "ZFS clone path: dep is clone",
			zfsCfg:          config.ZFS{Enabled: true, Pool: "tank"},
			cloneConfig:     &database.CloneConfig{SourceDatabaseID: "src-db", SourceNodeName: "n1"},
			expectedDepType: zfs.ResourceTypeClone,
		},
		{
			name:            "ZFS non-clone path: dep is dataset",
			zfsCfg:          config.ZFS{Enabled: true, Pool: "tank"},
			cloneConfig:     nil,
			expectedDepType: zfs.ResourceTypeDataset,
		},
		{
			name:            "non-ZFS path: dep is dir",
			zfsCfg:          config.ZFS{Enabled: false},
			cloneConfig:     nil,
			expectedDepType: filesystem.ResourceTypeDir,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orch := testOrchestrator(tt.zfsCfg)
			spec := testInstanceSpec(tt.cloneConfig)

			_, resources, err := orch.instanceResources(spec)
			require.NoError(t, err)

			serviceSpec := findServiceSpec(resources)
			require.NotNil(t, serviceSpec, "should emit a PostgresServiceSpecResource")

			assert.Equal(t, tt.expectedDepType, serviceSpec.DataDirDep.Type,
				"PostgresServiceSpecResource.DataDirDep.Type mismatch")
		})
	}
}

// findServiceSpec returns the PostgresServiceSpecResource from the resource
// list, or nil if not found.
func findServiceSpec(resources []resource.Resource) *PostgresServiceSpecResource {
	for _, r := range resources {
		if s, ok := r.(*PostgresServiceSpecResource); ok {
			return s
		}
	}
	return nil
}
