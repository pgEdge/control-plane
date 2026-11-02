package common

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/patroni"
)

func TestEtcdHostsOutdated(t *testing.T) {
	configWithHosts := func(hosts ...string) *patroni.Config {
		return &patroni.Config{
			Etcd3: &patroni.Etcd{Hosts: &hosts},
		}
	}

	for _, tc := range []struct {
		name      string
		config    *patroni.Config
		etcdHosts []string
		expected  bool
	}{
		{
			name:      "same hosts",
			config:    configWithHosts("host-1:2379", "host-2:2379", "host-3:2379"),
			etcdHosts: []string{"host-1:2379", "host-2:2379", "host-3:2379"},
			expected:  false,
		},
		{
			name:      "same hosts in different order",
			config:    configWithHosts("host-3:2379", "host-1:2379", "host-2:2379"),
			etcdHosts: []string{"host-1:2379", "host-2:2379", "host-3:2379"},
			expected:  false,
		},
		{
			name:      "host removed",
			config:    configWithHosts("host-1:2379", "host-2:2379", "host-3:2379", "host-4:2379"),
			etcdHosts: []string{"host-1:2379", "host-2:2379", "host-3:2379"},
			expected:  true,
		},
		{
			name:      "host added",
			config:    configWithHosts("host-1:2379", "host-2:2379"),
			etcdHosts: []string{"host-1:2379", "host-2:2379", "host-3:2379"},
			expected:  true,
		},
		{
			name:      "host replaced",
			config:    configWithHosts("host-1:2379", "host-2:2379", "host-3:2379"),
			etcdHosts: []string{"host-1:2379", "host-2:2379", "host-4:2379"},
			expected:  true,
		},
		{
			name:      "missing etcd3 section",
			config:    &patroni.Config{},
			etcdHosts: []string{"host-1:2379"},
			expected:  true,
		},
		{
			name:      "missing hosts",
			config:    &patroni.Config{Etcd3: &patroni.Etcd{}},
			etcdHosts: []string{"host-1:2379"},
			expected:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, etcdHostsOutdated(tc.config, tc.etcdHosts))
		})
	}
}
