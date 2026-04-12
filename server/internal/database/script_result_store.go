package database

import (
	"github.com/pgEdge/control-plane/server/internal/storage"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type StoredScriptResult struct {
	storage.StoredValue
	Result *ScriptResult `json:"script_result"`
}

type ScriptResultStore struct {
	client *clientv3.Client
	root   string
}

func NewScriptResultStore(client *clientv3.Client, root string) *ScriptResultStore {
	return &ScriptResultStore{
		client: client,
		root:   root,
	}
}

func (s *ScriptResultStore) Prefix() string {
	return storage.Prefix("/", s.root, "script_results")
}

func (s *ScriptResultStore) DatabasePrefix(databaseID string) string {
	return storage.Prefix(s.Prefix(), databaseID)
}

func (s *ScriptResultStore) ScriptNamePrefix(databaseID string, name ScriptName) string {
	return storage.Prefix(s.DatabasePrefix(databaseID), name.String())
}

func (s *ScriptResultStore) Key(databaseID string, scriptName ScriptName, nodeName string) string {
	return storage.Prefix(s.ScriptNamePrefix(databaseID, scriptName), nodeName)
}

func (s *ScriptResultStore) GetByKey(databaseID string, scriptName ScriptName, nodeName string) storage.GetOp[*StoredScriptResult] {
	key := s.Key(databaseID, scriptName, nodeName)
	return storage.NewGetOp[*StoredScriptResult](s.client, key)
}

func (s *ScriptResultStore) Update(item *StoredScriptResult) storage.PutOp[*StoredScriptResult] {
	key := s.Key(item.Result.DatabaseID, item.Result.ScriptName, item.Result.NodeName)
	return storage.NewUpdateOp(s.client, key, item)
}

func (s *ScriptResultStore) DeleteByDatabaseID(databaseID string) storage.DeleteOp {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}
