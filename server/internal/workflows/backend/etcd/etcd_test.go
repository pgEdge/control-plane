//go:build workflows_backend_test

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

	"github.com/pgEdge/control-plane/server/internal/storage/storagetest"
)

func Test_EtcdBackend(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	test.BackendTest(t, func(options ...backend.BackendOption) test.TestBackend {
		opts := backend.ApplyOptions(options...)
		return NewBackend(NewStore(client, uuid.NewString()), opts)
	}, nil)
}

func Test_EtcdBackendE2E(t *testing.T) {
	server := storagetest.NewEtcdTestServer(t)
	client := server.Client(t)

	test.EndToEndBackendTest(t, func(options ...backend.BackendOption) test.TestBackend {
		opts := backend.ApplyOptions(options...)
		return NewBackend(NewStore(client, uuid.NewString()), opts)
	}, nil)
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
