package etcd

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cschleiden/go-workflows/backend"
	"github.com/cschleiden/go-workflows/backend/history"
	"github.com/cschleiden/go-workflows/backend/test"

	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func Test_EtcdBackend(t *testing.T) {
	var server *storagetest.EtcdTestServer
	var client storage.EtcdClient

	test.BackendTest(t, func(options ...backend.BackendOption) test.TestBackend {
		server = storagetest.NewEtcdTestServer(t)
		client = server.Client()

		opts := backend.ApplyOptions(options...)
		return NewBackend(NewStore(client, uuid.NewString()), opts)
	}, func(b test.TestBackend) {
		server.Close()
		client.Close()
	})
}

func Test_EtcdBackendE2E(t *testing.T) {
	// TODO: Every test passes when run individually, and _sometimes_ they'll
	// all pass when run together. But, I've seen an intermittent issue where
	// the workflow executor Continue() method gets called after its Close()
	// method has been called, which causes a panic. I suspect this is a bug in
	// the E2E test implementation, but I'll need to follow up on it.
	t.Skip("These tests are flaky")

	var server *storagetest.EtcdTestServer
	var client storage.EtcdClient

	test.EndToEndBackendTest(t, func(options ...backend.BackendOption) test.TestBackend {
		server = storagetest.NewEtcdTestServer(t)
		client = server.Client()

		opts := backend.ApplyOptions(options...)
		return NewBackend(NewStore(client, uuid.NewString()), opts)
	}, func(b test.TestBackend) {
		server.Close()
		client.Close()
	})
}

func (b *Backend) GetFutureEvents(ctx context.Context) ([]*history.Event, error) {
	events, err := b.store.PendingEvent.
		GetAll().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending events: %w", err)
	}

	now := time.Now()
	var futureEvents []*history.Event
	for _, event := range events {
		if event.Event.VisibleAt != nil && event.Event.VisibleAt.After(now) {
			futureEvents = append(futureEvents, event.Event)
		}
	}

	return futureEvents, nil
}
