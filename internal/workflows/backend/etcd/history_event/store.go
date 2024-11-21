package history_event

import (
	"context"
	"fmt"
	"math"
	"path"

	"github.com/cschleiden/go-workflows/backend/history"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/internal/storage"
)

type Value struct {
	version             int64          `json:"-"`
	WorkflowInstanceID  string         `json:"workflow_instance_id"`
	WorkflowExecutionID string         `json:"workflow_execution_id"`
	Event               *history.Event `json:"event"`
}

func (v *Value) Version() int64 {
	return v.version
}

func (v *Value) SetVersion(version int64) {
	v.version = version
}

type Store struct {
	client storage.EtcdClient
	root   string
}

func NewStore(client storage.EtcdClient, root string) *Store {
	return &Store{
		client: client,
		root:   root,
	}
}

func (s *Store) InstanceExecutionPrefix(instanceID, executionID string) string {
	return path.Join("/", s.root, "workflows", "history_events", instanceID, executionID)
}

func (s *Store) Key(instanceID, executionID string, sequenceID int64) string {
	// We're formatting the sequence IDs in hex with leading zeros. This is
	// important so that we're able to properly use range queries, because etcd
	// sorts alphabetically, meaning that 10 comes before 9. The leading zeros
	// ensure that larger numbers are sorted after smaller numbers.
	return path.Join(s.InstanceExecutionPrefix(instanceID, executionID), fmt.Sprintf("%016x", sequenceID))
}

func (s *Store) GetLastSequenceID(ctx context.Context, instanceID, executionID string) (int64, error) {
	prefix := s.InstanceExecutionPrefix(instanceID, executionID)
	resp, err := s.client.Get(ctx, prefix,
		clientv3.WithPrefix(),
		clientv3.WithSort(clientv3.SortByKey, clientv3.SortDescend),
		clientv3.WithLimit(1),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to get last sequence ID for prefix %q: %w", prefix, err)
	}
	events, err := storage.DecodeGetResponse[*Value](resp)
	if err != nil {
		return 0, fmt.Errorf("failed to decode get response for prefix %q: %w", prefix, err)
	}
	if len(events) < 1 {
		return 0, nil
	}
	return events[0].Event.SequenceID, nil
}

func (s *Store) GetByKey(instanceID, executionID string, sequenceID int64) storage.GetOp[*Value] {
	key := s.Key(instanceID, executionID, sequenceID)
	return storage.NewGetOp[*Value](s.client, key)
}

func (s *Store) GetAfterSequenceID(instanceID, executionID string, lastSequenceID *int64) storage.GetMultipleOp[*Value] {
	var start string
	if lastSequenceID != nil {
		start = s.Key(instanceID, executionID, *lastSequenceID+1)
	} else {
		start = s.Key(instanceID, executionID, 1)
	}
	end := s.Key(instanceID, executionID, math.MaxInt64)
	return storage.NewGetRangeOp[*Value](s.client, start, end)
}

func (s *Store) Create(item *Value) storage.PutOp[*Value] {
	key := s.Key(item.WorkflowInstanceID, item.WorkflowExecutionID, item.Event.SequenceID)
	return storage.NewCreateOp(s.client, key, item)
}

func (s *Store) DeleteByKey(instanceID, executionID string, sequenceID int64) storage.DeleteOp {
	key := s.Key(instanceID, executionID, sequenceID)
	return storage.NewDeleteKeyOp(s.client, key)
}

func (s *Store) DeleteByInstanceExecution(instanceID, executionID string) storage.DeleteOp {
	prefix := s.InstanceExecutionPrefix(instanceID, executionID)
	return storage.NewDeletePrefixOp(s.client, prefix)
}
