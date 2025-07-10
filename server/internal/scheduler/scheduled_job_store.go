package scheduler

import (
	"path"

	"github.com/pgEdge/control-plane/server/internal/storage"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type ScheduledJobStore struct {
	client *clientv3.Client
	root   string
}

func NewScheduledJobStore(client *clientv3.Client, root string) *ScheduledJobStore {
	return &ScheduledJobStore{
		client: client,
		root:   root,
	}
}

func (s *ScheduledJobStore) Prefix() string {
	return path.Join("/", s.root, ScheduledJobPrefix)
}

func (s *ScheduledJobStore) Key(id string) string {
	return path.Join(s.Prefix(), id)
}
func (s *ScheduledJobStore) Put(job *StoredScheduledJob) storage.PutOp[*StoredScheduledJob] {
	return storage.NewPutOp(s.client, s.Key(job.ID), job)
}

func (s *ScheduledJobStore) Get(jobID string) storage.GetOp[*StoredScheduledJob] {
	return storage.NewGetOp[*StoredScheduledJob](s.client, s.Key(jobID))
}
func (s *ScheduledJobStore) GetAll() storage.GetMultipleOp[*StoredScheduledJob] {
	return storage.NewGetPrefixOp[*StoredScheduledJob](s.client, s.Prefix())
}
func (s *ScheduledJobStore) Delete(jobID string) storage.DeleteOp {
	return storage.NewDeleteKeyOp(s.client, s.Key(jobID))
}

func (s *ScheduledJobStore) WatchJobs() storage.WatchOp[*StoredScheduledJob] {
	return storage.NewWatchPrefixOp[*StoredScheduledJob](s.client, s.Prefix())
}
