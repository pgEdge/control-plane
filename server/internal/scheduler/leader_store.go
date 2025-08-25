package scheduler

import (
	"time"

	"github.com/pgEdge/control-plane/server/internal/storage"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type StoredLeader struct {
	storage.StoredValue
	HostID    string
	CreatedAt time.Time
}

type LeaderStore struct {
	client *clientv3.Client
	root   string
}

func NewLeaderStore(client *clientv3.Client, root string) *LeaderStore {
	return &LeaderStore{
		client: client,
		root:   root,
	}
}

func (s *LeaderStore) Key() string {
	return storage.Prefix("/", s.root, SchedulerLeaderPrefix, "leader")
}

func (s *LeaderStore) GetByKey() storage.GetOp[*StoredLeader] {
	return storage.NewGetOp[*StoredLeader](s.client, s.Key())
}

func (s *LeaderStore) Create(item *StoredLeader) storage.PutOp[*StoredLeader] {
	return storage.NewCreateOp(s.client, s.Key(), item)
}

func (s *LeaderStore) Update(item *StoredLeader) storage.PutOp[*StoredLeader] {
	return storage.NewUpdateOp(s.client, s.Key(), item)
}

func (s *LeaderStore) Watch() storage.WatchOp[*StoredLeader] {
	return storage.NewWatchOp[*StoredLeader](s.client, s.Key())
}
