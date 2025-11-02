package resource

import (
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/storage"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type StoredPlanSummaries struct {
	storage.StoredValue
	DatabaseID string        `json:"database_id"`
	TaskID     uuid.UUID     `json:"task_id"`
	Plans      []PlanSummary `json:"plans"`
}

type PlanSummaryStore struct {
	client *clientv3.Client
	root   string
}

func NewPlanSummaryStore(client *clientv3.Client, root string) *PlanSummaryStore {
	return &PlanSummaryStore{
		client: client,
		root:   root,
	}
}

func (s *PlanSummaryStore) Prefix() string {
	return storage.Prefix("/", s.root, "plan_summaries")
}

func (s *PlanSummaryStore) DatabasePrefix(databaseID string) string {
	return storage.Key(s.Prefix(), databaseID)
}

func (s *PlanSummaryStore) Key(databaseID string, taskID uuid.UUID) string {
	return storage.Key(s.DatabasePrefix(databaseID), taskID.String())
}

func (s *PlanSummaryStore) GetByKey(databaseID string, taskID uuid.UUID) storage.GetOp[*StoredPlanSummaries] {
	key := s.Key(databaseID, taskID)
	return storage.NewGetOp[*StoredPlanSummaries](s.client, key)
}

func (s *PlanSummaryStore) Put(item *StoredPlanSummaries) storage.PutOp[*StoredPlanSummaries] {
	key := s.Key(item.DatabaseID, item.TaskID)
	return storage.NewPutOp(s.client, key, item)
}

func (s *PlanSummaryStore) DeleteByDatabaseID(databaseID string) storage.DeleteOp {
	prefix := s.DatabasePrefix(databaseID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}
