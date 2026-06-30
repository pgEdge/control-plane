package task

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/storage"
)

var (
	ErrTaskCanceled = errors.New("task was canceled")
	ErrTaskFailed   = errors.New("task failed")
)

// Watcher is a subscription to a task's terminal state. Multiple Watchers for
// the same task share a single underlying etcd watch stream.
type Watcher struct {
	mu     sync.Mutex
	closed bool
	err    error
	done   chan struct{}
	errCh  chan error
	shared *sharedWatcher
}

// Done returns a channel that is closed when the task reaches a terminal state
// or is deleted.
func (w *Watcher) Done() <-chan struct{} {
	return w.done
}

// Err returns nil if the task completed successfully, ErrTaskCanceled if it
// was canceled (or is canceling), or ErrTaskFailed if it failed. It is only
// meaningful after Done() is closed.
func (w *Watcher) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

// Close releases this subscription. When the last subscription for a task is
// closed, the underlying etcd watch stream is stopped.
func (w *Watcher) Close() {
	w.shared.release(w)
}

// Error returns a channel that receives an error if the underlying watch
// stream fails. The channel carries at most one value. Callers that select on
// Done should also select on Error so they are not blocked when the watch
// stream dies before the task reaches a terminal state.
func (w *Watcher) Error() <-chan error {
	return w.errCh
}

func (w *Watcher) finish(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	w.err = err
	close(w.done)
}

// sharedWatcher holds one etcd watch stream for a task and fans events out to
// all active Watcher subscriptions. It is managed by watcherRegistry.
type sharedWatcher struct {
	mu           sync.Mutex
	subscribers  []*Watcher
	terminal     bool
	terminalErr  error
	watchOp      storage.WatchOp[*StoredTask]
	registry     *watcherRegistry
	taskID       uuid.UUID
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
	cancelWatch  context.CancelFunc
}

// newSubscription creates and registers a new Watcher. If the task is already
// in a terminal state, the returned Watcher's Done channel is closed immediately.
func (sw *sharedWatcher) newSubscription() *Watcher {
	w := &Watcher{
		done:   make(chan struct{}),
		errCh:  make(chan error, 1),
		shared: sw,
	}
	sw.mu.Lock()
	sw.subscribers = append(sw.subscribers, w)
	if sw.terminal {
		w.closed = true
		w.err = sw.terminalErr
		close(w.done)
	}
	sw.mu.Unlock()
	return w
}

func (sw *sharedWatcher) finishAll(err error) {
	sw.mu.Lock()
	sw.terminal = true
	sw.terminalErr = err
	subs := make([]*Watcher, len(sw.subscribers))
	copy(subs, sw.subscribers)
	sw.mu.Unlock()
	for _, sub := range subs {
		sub.finish(err)
	}
}

func (sw *sharedWatcher) handleEvent(e *storage.Event[*StoredTask]) error {
	switch e.Type {
	case storage.EventTypeDelete:
		sw.finishAll(ErrTaskCanceled)
	case storage.EventTypeError:
		return e.Err
	case storage.EventTypePut:
		if e.Value == nil || e.Value.Task == nil {
			return nil
		}
		switch e.Value.Task.Status {
		case StatusCanceled, StatusCanceling:
			sw.finishAll(ErrTaskCanceled)
		case StatusFailed:
			sw.finishAll(ErrTaskFailed)
		case StatusCompleted:
			sw.finishAll(nil)
		}
	}
	return nil
}

// propagateErrors forwards watch stream errors to all active subscriptions.
// context.Canceled is filtered out — it indicates normal cleanup when
// cancelWatch is called and should not be surfaced as an error.
func (sw *sharedWatcher) propagateErrors() {
	select {
	case <-sw.shutdownCh:
	case err := <-sw.watchOp.Error():
		if errors.Is(err, context.Canceled) {
			return
		}
		sw.mu.Lock()
		subs := make([]*Watcher, len(sw.subscribers))
		copy(subs, sw.subscribers)
		sw.mu.Unlock()
		for _, w := range subs {
			select {
			case w.errCh <- err:
			default:
			}
		}
	}
}

// release removes w from the subscriber list. When the last subscriber is
// removed, it stops the underlying watch stream and removes the sharedWatcher
// from the registry.
//
// sw.mu is always released before sw.registry.mu is acquired so that
// watcherRegistry.acquire (which holds registry.mu and may acquire sw.mu via
// newSubscription) cannot deadlock with release.
func (sw *sharedWatcher) release(w *Watcher) {
	sw.mu.Lock()
	for i, sub := range sw.subscribers {
		if sub == w {
			sw.subscribers = slices.Delete(sw.subscribers, i, i+1)
			break
		}
	}
	remaining := len(sw.subscribers)
	sw.mu.Unlock()

	if remaining == 0 {
		sw.shutdown()
	}
}

func (sw *sharedWatcher) shutdown() {
	sw.shutdownOnce.Do(func() {
		sw.registry.mu.Lock()
		delete(sw.registry.entries, sw.taskID)
		sw.registry.mu.Unlock()
		close(sw.shutdownCh)
		sw.cancelWatch()
		sw.watchOp.Close()
	})
}

// watcherRegistry maintains at most one shared watch stream per task across
// all concurrent callers on the same service instance.
type watcherRegistry struct {
	mu      sync.Mutex
	entries map[uuid.UUID]*sharedWatcher
}

func newWatcherRegistry() *watcherRegistry {
	return &watcherRegistry{
		entries: make(map[uuid.UUID]*sharedWatcher),
	}
}

func (r *watcherRegistry) acquire(store *TaskStore, scope Scope, entityID string, taskID uuid.UUID) (*Watcher, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if sw, ok := r.entries[taskID]; ok {
		return sw.newSubscription(), nil
	}

	watchCtx, cancelWatch := context.WithCancel(context.Background())
	watchOp := store.Watch(scope, entityID, taskID)
	sw := &sharedWatcher{
		watchOp:     watchOp,
		registry:    r,
		taskID:      taskID,
		shutdownCh:  make(chan struct{}),
		cancelWatch: cancelWatch,
	}

	// Create the first subscription before starting the watch so that
	// handleEvent's synchronous load() call can signal it if the task is
	// already terminal.
	w := sw.newSubscription()

	if err := watchOp.Watch(watchCtx, sw.handleEvent); err != nil {
		cancelWatch()
		return nil, fmt.Errorf("failed to start task watcher: %w", err)
	}

	r.entries[taskID] = sw
	go sw.propagateErrors()
	return w, nil
}
