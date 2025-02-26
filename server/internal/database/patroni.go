package database

import (
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/patroni"
)

// const patroniEtcdNamespace = "/patroni"
const defaultTTL = 30
const defaultLoopWait = 10
const defaultRetryTimeout = 10

func (s *Spec) PatroniConfig(cfg *config.Config) (*patroni.Config, error) {
	return nil, nil
	// hostCfg, err := s.HostConfig(cfg.HostID)
	// if err != nil {
	// 	return nil, err
	// }

	// return &patroni.Config{
	// 	Namespace: utils.PointerTo(patroni.Namespace),
	// 	Scope:     utils.PointerTo(fmt.Sprintf("%s_%s", hostCfg.ID, hostCfg.NodeName)),
	// 	Name:      utils.PointerTo(hostCfg.HostID),
	// 	Log: &patroni.Log{
	// 		Type: utils.PointerTo(patroni.LogTypeJson),
	// 		StaticFields: &map[string]string{
	// 			"tenant_id":   hostCfg.TenantID,
	// 			"database_id": hostCfg.ID,
	// 			"host_id":     hostCfg.HostID,
	// 			"node_name":   hostCfg.NodeName,
	// 		},
	// 	},
	// 	Bootstrap: &patroni.Bootstrap{
	// 		DCS: &patroni.DCS{
	// 			Postgresql: &patroni.DCSPostgreSQL{
	// 				Parameters: &map[string]any{
	// 					"wal_level": "logical",
	// 				},
	// 			},
	// 			IgnoreSlots: &[]patroni.IgnoreSlot{
	// 				{
	// 					Type: utils.PointerTo(patroni.SlotTypeLogical),
	// 				},
	// 			},
	// 			TTL:          utils.PointerTo(defaultTTL),
	// 			LoopWait:     utils.PointerTo(defaultLoopWait),
	// 			RetryTimeout: utils.PointerTo(defaultRetryTimeout),
	// 		},
	// 	},
	// 	// TODO:
	// 	Etcd3: &patroni.Etcd{
	// 		// Hosts: ,
	// 	},
	// 	Postgresql: &patroni.PostgreSQL{
	// 		// TODO:
	// 		// Authentication: &patroni.Authentication{
	// 		// 	Superuser: &patroni.User{
	// 		// 		Username: utils.PointerTo(""),
	// 		// 		Password: utils.PointerTo(""),
	// 		// 	},
	// 		// 	Replication: &patroni.User{
	// 		// 		Username: utils.PointerTo(""),
	// 		// 		Password: utils.PointerTo(""),
	// 		// 	},
	// 		// },
	// 		Callbacks: &patroni.Callbacks{
	// 			// TODO:
	// 			// OnStart: utils.PointerTo(""),
	// 		},
	// 		// TODO:
	// 		// DataDir: utils.PointerTo(""),
	// 	},
	// }, nil
}
