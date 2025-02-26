package postgres_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/postgres"
)

func TestDefaultTunableGUCs(t *testing.T) {
	for _, tc := range []struct {
		name        string
		memBytes    uint64
		cpus        float64
		clusterSize int
		expected    map[string]any
	}{
		{
			name:        "r7g.medium 3 node",
			memBytes:    8 * (1024 * 1024 * 1024),
			cpus:        1,
			clusterSize: 3,
			expected: map[string]any{
				"autovacuum_max_workers":       3,
				"autovacuum_vacuum_cost_limit": 200,
				"autovacuum_work_mem":          int64(262144), // * KB = 256MB
				"effective_cache_size":         int64(524288), // * BLKSZ = 4GB
				"maintenance_work_mem":         int64(137518), // * KB = 134MB
				"max_connections":              901,
				"max_worker_processes":         12,
				"max_parallel_workers":         8,
				"shared_buffers":               int64(262144), // * KB = 256MB
				"max_wal_senders":              16,
				"max_replication_slots":        16,
			},
		},
		{
			name:        "r7g.medium 20 node", // This is ridiculous, but just in case...
			memBytes:    8 * (1024 * 1024 * 1024),
			cpus:        1,
			clusterSize: 20,
			expected: map[string]any{
				"autovacuum_max_workers":       3,
				"autovacuum_vacuum_cost_limit": 200,
				"autovacuum_work_mem":          int64(262144), // * KB = 256MB
				"effective_cache_size":         int64(524288), // * BLKSZ = 4GB
				"maintenance_work_mem":         int64(137518), // * KB = ~134MB
				"max_connections":              901,
				"max_worker_processes":         20,
				"max_parallel_workers":         8,
				"shared_buffers":               int64(262144), // * KB = 256MB
				"max_wal_senders":              22,
				"max_replication_slots":        22,
			},
		},
		{
			name:        "r7g.large 3 node",
			memBytes:    16 * (1024 * 1024 * 1024),
			cpus:        2,
			clusterSize: 3,
			expected: map[string]any{
				"autovacuum_max_workers":       3,
				"autovacuum_vacuum_cost_limit": 200,
				"autovacuum_work_mem":          int64(524288),  // * KB = 512MB
				"effective_cache_size":         int64(1048576), // * BLKSZ = 8GB
				"maintenance_work_mem":         int64(275036),  // * KB = ~269MB
				"max_connections":              1802,
				"max_worker_processes":         12,
				"max_parallel_workers":         8,
				"shared_buffers":               int64(524288), // * KB = 512MB
				"max_wal_senders":              16,
				"max_replication_slots":        16,
			},
		},
		{
			name:        "r7g.xlarge 3 node",
			memBytes:    32 * (1024 * 1024 * 1024),
			cpus:        4,
			clusterSize: 3,
			expected: map[string]any{
				"autovacuum_max_workers":       3,
				"autovacuum_vacuum_cost_limit": 406,
				"autovacuum_work_mem":          int64(1048576), // * KB = 1GB
				"effective_cache_size":         int64(2097152), // * BLKSZ = 16GB
				"maintenance_work_mem":         int64(550072),  // * KB = ~537MB
				"max_connections":              3604,
				"max_worker_processes":         12,
				"max_parallel_workers":         8,
				"shared_buffers":               int64(1048576), // * KB = 1GB
				"max_wal_senders":              16,
				"max_replication_slots":        16,
			},
		},
		{
			name:        "r7g.2xlarge 3 node",
			memBytes:    64 * (1024 * 1024 * 1024),
			cpus:        8,
			clusterSize: 3,
			expected: map[string]any{
				"autovacuum_max_workers":       3,
				"autovacuum_vacuum_cost_limit": 1006,
				"autovacuum_work_mem":          int64(2097152), // * KB = 2GB
				"effective_cache_size":         int64(4194304), // * BLKSZ = 32GB
				"maintenance_work_mem":         int64(1100145), // * KB = ~1074MB
				"max_connections":              5000,
				"max_worker_processes":         16,
				"max_parallel_workers":         8,
				"shared_buffers":               int64(2097152), // * KB = 2GB
				"max_wal_senders":              16,
				"max_replication_slots":        16,
			},
		},
		{
			name:        "r7g.4xlarge 3 node",
			memBytes:    128 * (1024 * 1024 * 1024),
			cpus:        16,
			clusterSize: 3,
			expected: map[string]any{
				"autovacuum_max_workers":       3,
				"autovacuum_vacuum_cost_limit": 1606,
				"autovacuum_work_mem":          int64(4194304), // * KB = 4GB
				"effective_cache_size":         int64(8388608), // * BLKSZ = 64GB
				"maintenance_work_mem":         int64(2200290), // * KB = ~2149MB
				"max_connections":              5000,
				"max_worker_processes":         32,
				"max_parallel_workers":         8,
				"shared_buffers":               int64(4194304), // * KB = 4GB
				"max_wal_senders":              16,
				"max_replication_slots":        16,
			},
		},
		{
			name:        "r7g.8xlarge 3 node",
			memBytes:    256 * (1024 * 1024 * 1024),
			cpus:        32,
			clusterSize: 3,
			expected: map[string]any{
				"autovacuum_max_workers":       4,
				"autovacuum_vacuum_cost_limit": 2206,
				"autovacuum_work_mem":          int64(8388608),  // * KB = 8GB
				"effective_cache_size":         int64(16777216), // * BLKSZ = 128GB
				"maintenance_work_mem":         int64(4400581),  // * KB = ~4297MB
				"max_connections":              5000,
				"max_worker_processes":         64,
				"max_parallel_workers":         16,
				"shared_buffers":               int64(8388608), // * KB = 8GB
				"max_wal_senders":              16,
				"max_replication_slots":        16,
			},
		},
		{
			name:        "n2-highmem-4/100 3 node",       // Equitable distribution of DE cluster
			memBytes:    32 * (1024 * 1024 * 1024) / 100, // ~328MB
			cpus:        4.0 / 100,
			clusterSize: 3,
			expected: map[string]any{
				"autovacuum_max_workers":       3,
				"autovacuum_vacuum_cost_limit": 200,
				"autovacuum_work_mem":          int64(65536), // * KB = 64MB
				"effective_cache_size":         int64(20971), // * BLKSZ = ~164MB
				"maintenance_work_mem":         int64(65536), // * KB = 64MB
				"max_connections":              36,
				"max_worker_processes":         12,
				"max_parallel_workers":         8,
				"shared_buffers":               int64(10485), // * KB = ~10MB
				"max_wal_senders":              16,
				"max_replication_slots":        16,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, postgres.DefaultTunableGUCs(tc.memBytes, tc.cpus, tc.clusterSize))
		})
	}
}
