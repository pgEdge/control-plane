package etcd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/backend"
	"github.com/cschleiden/go-workflows/backend/history"
	"github.com/cschleiden/go-workflows/backend/metrics"
	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/pgEdge/control-plane/internal/storage"
	"github.com/pgEdge/control-plane/internal/workflows/backend/etcd/activity_lock"
	"github.com/pgEdge/control-plane/internal/workflows/backend/etcd/activity_queue_item"
	"github.com/pgEdge/control-plane/internal/workflows/backend/etcd/history_event"
	"github.com/pgEdge/control-plane/internal/workflows/backend/etcd/pending_event"
	"github.com/pgEdge/control-plane/internal/workflows/backend/etcd/workflow_instance"
	"github.com/pgEdge/control-plane/internal/workflows/backend/etcd/workflow_instance_lock"
	"github.com/pgEdge/control-plane/internal/workflows/backend/etcd/workflow_instance_sticky"
	"github.com/pgEdge/control-plane/internal/workflows/backend/etcd/workflow_queue_item"
)

var _ backend.Backend = (*Backend)(nil)

type Backend struct {
	store    *Store
	options  *backend.Options
	workerID string
}

func NewBackend(store *Store, options *backend.Options) *Backend {
	return &Backend{
		store:    store,
		options:  options,
		workerID: uuid.NewString(),
	}
}

func (b *Backend) CreateWorkflowInstance(ctx context.Context, instance *workflow.Instance, event *history.Event) error {
	// Check for existing active instance execution
	instances, err := b.store.WorkflowInstance.
		GetByInstanceID(instance.InstanceID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to check for existing active instance: %w", err)
	}
	for _, inst := range instances {
		if inst.State == core.WorkflowInstanceStateActive {
			return backend.ErrInstanceAlreadyExists
		}
	}

	attrs := event.Attributes.(*history.ExecutionStartedAttributes)
	err = b.store.Txn(
		b.store.WorkflowInstance.Create(&workflow_instance.Value{
			WorkflowInstance: instance,
			CreatedAt:        time.Now(),
			Queue:            attrs.Queue,
			Metadata:         attrs.Metadata,
			State:            core.WorkflowInstanceStateActive,
		}),
		b.store.PendingEvent.Put(&pending_event.Value{
			WorkflowInstanceID:  instance.InstanceID,
			WorkflowExecutionID: instance.ExecutionID,
			Event:               event,
		}),
		b.store.WorkflowQueueItem.Put(&workflow_queue_item.Value{
			WorkflowInstance: instance,
			CreatedAt:        time.Now(),
			Queue:            attrs.Queue,
			Metadata:         attrs.Metadata,
			State:            core.WorkflowInstanceStateActive,
		}),
	).Commit(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrOperationConstraintViolated) {
			return backend.ErrInstanceAlreadyExists
		}
		return fmt.Errorf("failed to create workflow instance: %w", err)
	}

	return nil
}

func (b *Backend) CancelWorkflowInstance(ctx context.Context, instance *workflow.Instance, cancelEvent *history.Event) error {
	// Validate that workflow exists
	exists, err := b.store.WorkflowInstance.
		ExistsByKey(instance.InstanceID, instance.ExecutionID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to get workflow instance: %w", err)
	}
	if !exists {
		return backend.ErrInstanceNotFound
	}

	err = b.store.PendingEvent.
		Create(&pending_event.Value{
			WorkflowInstanceID:  instance.InstanceID,
			WorkflowExecutionID: instance.ExecutionID,
			Event:               cancelEvent,
		}).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to create pending event: %w", err)
	}

	return nil
}

func (b *Backend) RemoveWorkflowInstance(ctx context.Context, instance *workflow.Instance) error {
	inst, err := b.store.WorkflowInstance.
		GetByKey(instance.InstanceID, instance.ExecutionID).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return backend.ErrInstanceNotFound
	} else if err != nil {
		return fmt.Errorf("failed to get workflow instance: %w", err)
	}

	if inst.State != core.WorkflowInstanceStateFinished && inst.State != core.WorkflowInstanceStateContinuedAsNew {
		return backend.ErrInstanceNotFinished
	}

	err = b.store.Txn(
		b.store.WorkflowInstance.DeleteByKey(instance.InstanceID, instance.ExecutionID),
		b.store.HistoryEvent.DeleteByInstanceExecution(instance.InstanceID, instance.ExecutionID),
	).Commit(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete workflow instance: %w", err)
	}

	return nil
}

func (b *Backend) RemoveWorkflowInstances(ctx context.Context, options ...backend.RemovalOption) error {
	ro := backend.DefaultRemovalOptions
	for _, opt := range options {
		opt(&ro)
	}

	instances, err := b.store.WorkflowInstance.
		GetAll().
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to get all instances: %w", err)
	}

	var ops []storage.TxnOperation
	for _, instance := range instances {
		if instance.State == core.WorkflowInstanceStateActive {
			continue
		}
		instanceID := instance.WorkflowInstance.InstanceID
		executionID := instance.WorkflowInstance.ExecutionID
		ops = append(ops,
			b.store.WorkflowInstance.DeleteByKey(instanceID, executionID),
			b.store.HistoryEvent.DeleteByInstanceExecution(instanceID, executionID),
		)
	}

	err = b.store.Txn(ops...).Commit(ctx)
	if err != nil {
		return fmt.Errorf("failed to remove workflow instances: %w", err)
	}

	return nil
}

func (b *Backend) GetWorkflowInstanceState(ctx context.Context, instance *workflow.Instance) (core.WorkflowInstanceState, error) {
	inst, err := b.store.WorkflowInstance.
		GetByKey(instance.InstanceID, instance.ExecutionID).
		Exec(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return 0, backend.ErrInstanceNotFound
		}
		return 0, fmt.Errorf("failed to get instance: %w", err)
	}

	return inst.State, nil
}

func (b *Backend) GetWorkflowInstanceHistory(ctx context.Context, instance *workflow.Instance, lastSequenceID *int64) ([]*history.Event, error) {
	events, err := b.store.HistoryEvent.
		GetAfterSequenceID(instance.InstanceID, instance.ExecutionID, lastSequenceID).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}

	out := make([]*history.Event, len(events))
	for idx, event := range events {
		out[idx] = event.Event
	}

	return out, nil
}

func (b *Backend) SignalWorkflow(ctx context.Context, instanceID string, event *history.Event) error {
	instances, err := b.store.WorkflowInstance.
		GetByInstanceID(instanceID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to get instances: %w", err)
	}

	for _, instance := range instances {
		if instance.State != core.WorkflowInstanceStateActive {
			continue
		}
		executionID := instance.WorkflowInstance.ExecutionID
		err = b.store.PendingEvent.
			Create(&pending_event.Value{
				WorkflowInstanceID:  instanceID,
				WorkflowExecutionID: executionID,
				Event:               event,
			}).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to signal workflow: %w", err)
		}
		return nil
	}

	return backend.ErrInstanceNotFound
}

func (b *Backend) PrepareWorkflowQueues(ctx context.Context, queues []workflow.Queue) error {
	return nil
}

func (b *Backend) PrepareActivityQueues(ctx context.Context, queues []workflow.Queue) error {
	return nil
}

func (b *Backend) GetWorkflowTask(ctx context.Context, queues []workflow.Queue) (*backend.WorkflowTask, error) {
	for _, queue := range queues {
		items, err := b.store.WorkflowQueueItem.
			GetByQueue(string(queue)).
			Exec(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get queue items: %w", err)
		}
		for _, item := range items {
			instanceID := item.WorkflowInstance.InstanceID
			executionID := item.WorkflowInstance.ExecutionID

			locked, err := b.store.WorkflowInstanceLock.
				ExistsByKey(item.WorkflowInstance.InstanceID, item.WorkflowInstance.ExecutionID).
				Exec(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to check for lock: %w", err)
			}
			if locked {
				continue
			}
			sticky, err := b.store.WorkflowInstanceSticky.
				GetByKey(item.WorkflowInstance.InstanceID).
				Exec(ctx)
			if err != nil && !errors.Is(err, storage.ErrNotFound) {
				return nil, fmt.Errorf("failed to check for sticky: %w", err)
			}
			if sticky != nil && sticky.WorkerID != b.workerID {
				continue
			}
			pendingEvents, err := b.store.PendingEvent.
				GetByInstanceExecution(instanceID, executionID).
				Exec(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to get pending events: %w", err)
			}
			now := time.Now()
			var newEvents []*history.Event
			for _, event := range pendingEvents {
				// Skip events that aren't visible yet.
				if event.Event.VisibleAt != nil && event.Event.VisibleAt.After(now) {
					continue
				}
				newEvents = append(newEvents, event.Event)
			}
			if len(newEvents) < 1 {
				// No work to be done
				continue
			}
			err = b.store.WorkflowInstanceLock.
				Create(&workflow_instance_lock.Value{
					WorkflowInstanceID:  instanceID,
					WorkflowExecutionID: executionID,
					CreatedAt:           time.Now(),
				}).
				WithTTL(b.options.WorkflowLockTimeout).
				Exec(ctx)
			if err != nil {
				if errors.Is(err, storage.ErrAlreadyExists) {
					// Another worker managed to lock this item first
					continue
				}
				return nil, fmt.Errorf("failed to create workflow instance lock: %w", err)
			}
			err = b.store.WorkflowInstanceSticky.
				Put(&workflow_instance_sticky.Value{
					WorkflowInstanceID: instanceID,
					CreatedAt:          time.Now(),
					WorkerID:           b.workerID,
				}).
				WithTTL(b.options.StickyTimeout).
				Exec(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to create workflow instance sticky: %w", err)
			}
			lastSequenceID, err := b.store.HistoryEvent.
				GetLastSequenceID(ctx, instanceID, executionID)
			if err != nil {
				return nil, fmt.Errorf("failed to get last sequence ID: %w", err)
			}
			return &backend.WorkflowTask{
				ID:                    instanceID,
				WorkflowInstance:      item.WorkflowInstance,
				WorkflowInstanceState: item.State,
				Queue:                 item.Queue,
				Metadata:              item.Metadata,
				LastSequenceID:        lastSequenceID,
				NewEvents:             newEvents,
			}, nil
		}
	}

	return nil, nil
}

func (b *Backend) ExtendWorkflowTask(ctx context.Context, task *backend.WorkflowTask) error {
	instanceID := task.WorkflowInstance.InstanceID
	executionID := task.WorkflowInstance.ExecutionID
	lock, err := b.store.WorkflowInstanceLock.
		GetByKey(instanceID, executionID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to get lock: %w", err)
	}
	err = b.store.WorkflowInstanceLock.
		Update(lock).
		WithTTL(b.options.WorkflowLockTimeout).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update lock: %w", err)
	}

	return nil
}

func (b *Backend) CompleteWorkflowTask(
	ctx context.Context,
	task *backend.WorkflowTask,
	state core.WorkflowInstanceState,
	executedEvents, activityEvents, timerEvents []*history.Event,
	workflowEvents []*history.WorkflowEvent,
) error {
	queue := string(task.Queue)
	instanceID := task.WorkflowInstance.InstanceID
	executionID := task.WorkflowInstance.ExecutionID

	instance, err := b.store.WorkflowInstance.
		GetByKey(instanceID, executionID).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return backend.ErrInstanceNotFound
	} else if err != nil {
		return fmt.Errorf("failed to get queued workflow instance: %w", err)
	}

	ops := []storage.TxnOperation{
		b.store.WorkflowInstanceLock.DeleteByKey(instanceID, executionID),
	}
	futureEvents, err := b.store.PendingEvent.
		GetByInstanceExecution(instanceID, executionID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to get future events: %w", err)
	}
	for _, event := range executedEvents {
		ops = append(ops,
			b.store.PendingEvent.DeleteByKey(instanceID, executionID, event.ID),
			b.store.HistoryEvent.Create(&history_event.Value{
				WorkflowInstanceID:  instanceID,
				WorkflowExecutionID: executionID,
				Event:               event,
			}),
		)
		if event.Type == history.EventType_TimerCanceled {
			for _, futureEvent := range futureEvents {
				if futureEvent.Event.ScheduleEventID == event.ScheduleEventID {
					ops = append(ops,
						b.store.PendingEvent.DeleteByKey(instanceID, executionID, futureEvent.Event.ID),
					)
				}
			}
		}
	}
	for _, event := range activityEvents {
		attrs := event.Attributes.(*history.ActivityScheduledAttributes)
		queue := attrs.Queue
		if queue == "" {
			// Default to workflow queue
			queue = task.Queue
		}
		ops = append(ops,
			b.store.ActivityQueueItem.
				Create(&activity_queue_item.Value{
					WorkflowInstanceID:  instanceID,
					WorkflowExecutionID: executionID,
					Queue:               string(queue),
					Event:               event,
				}),
		)
	}
	for _, event := range timerEvents {
		ops = append(ops,
			b.store.PendingEvent.Create(&pending_event.Value{
				WorkflowInstanceID:  instanceID,
				WorkflowExecutionID: executionID,
				Event:               event,
			}),
		)
	}
	groupedEvents := history.EventsByWorkflowInstance(workflowEvents)
	for targetInstance, events := range groupedEvents {
		// Are we creating a new sub-workflow instance?
		first := events[0]
		if first.HistoryEvent.Type == history.EventType_WorkflowExecutionStarted {
			attrs := first.HistoryEvent.Attributes.(*history.ExecutionStartedAttributes)
			queue := attrs.Queue
			if queue == "" {
				queue = task.Queue
			}
			exists, err := b.store.WorkflowInstance.
				ExistsByKey(first.WorkflowInstance.InstanceID, first.WorkflowInstance.ExecutionID).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to check for existing sub-workflow instance: %w", err)
			}
			if exists {
				ops = append(ops,
					b.store.PendingEvent.
						Create(&pending_event.Value{
							WorkflowInstanceID:  instanceID,
							WorkflowExecutionID: executionID,
							Event: history.NewPendingEvent(
								time.Now(),
								history.EventType_SubWorkflowFailed,
								&history.SubWorkflowFailedAttributes{
									// TODO: Need to move workflowerrors out of internal
									// Error: workflow.NewError(backend.ErrInstanceAlreadyExists),
								},
								history.ScheduleEventID(first.WorkflowInstance.ParentEventID),
							),
						}),
				)
				continue
			}
			ops = append(ops,
				b.store.WorkflowInstance.
					Create(&workflow_instance.Value{
						WorkflowInstance: first.WorkflowInstance,
						CreatedAt:        time.Now(),
						Queue:            queue,
						Metadata:         attrs.Metadata,
						State:            core.WorkflowInstanceStateActive,
					}),
				b.store.WorkflowQueueItem.
					Create(&workflow_queue_item.Value{
						WorkflowInstance: first.WorkflowInstance,
						State:            core.WorkflowInstanceStateActive,
						CreatedAt:        time.Now(),
						Queue:            queue,
						Metadata:         attrs.Metadata,
					}),
			)
		}

		for _, event := range events {
			ops = append(ops,
				b.store.PendingEvent.Create(&pending_event.Value{
					WorkflowInstanceID:  targetInstance.InstanceID,
					WorkflowExecutionID: targetInstance.ExecutionID,
					Event:               event.HistoryEvent,
				}),
			)
		}
	}

	if b.options.RemoveContinuedAsNewInstances && state == core.WorkflowInstanceStateContinuedAsNew {
		ops = append(ops,
			b.store.WorkflowInstance.DeleteByKey(instanceID, executionID),
			b.store.WorkflowQueueItem.DeleteByKey(queue, instanceID, executionID),
		)
	} else if state == core.WorkflowInstanceStateContinuedAsNew || state == core.WorkflowInstanceStateFinished {
		now := time.Now()
		instance.State = state
		instance.FinishedAt = &now

		ops = append(ops,
			b.store.WorkflowQueueItem.DeleteByKey(queue, instanceID, executionID),
			b.store.WorkflowInstance.Update(instance),
			b.store.WorkflowInstanceSticky.
				Put(&workflow_instance_sticky.Value{
					WorkflowInstanceID: instanceID,
					CreatedAt:          time.Now(),
					WorkerID:           b.workerID,
				}).
				WithTTL(b.options.StickyTimeout),
		)
	} else {
		instance.State = state
		ops = append(ops,
			b.store.WorkflowInstance.Update(instance),
			b.store.WorkflowInstanceSticky.
				Put(&workflow_instance_sticky.Value{
					WorkflowInstanceID: instanceID,
					CreatedAt:          time.Now(),
					WorkerID:           b.workerID,
				}).
				WithTTL(b.options.StickyTimeout),
		)
	}

	err = b.store.Txn(ops...).Commit(ctx)
	if err != nil {
		return fmt.Errorf("failed to persist workflow task completion: %w", err)
	}

	return nil
}

func (b *Backend) GetActivityTask(ctx context.Context, queues []workflow.Queue) (*backend.ActivityTask, error) {
	for _, queue := range queues {
		items, err := b.store.ActivityQueueItem.
			GetByQueue(string(queue)).
			Exec(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get queue items: %w", err)
		}
		for _, item := range items {
			instanceID := item.WorkflowInstanceID
			executionID := item.WorkflowExecutionID
			locked, err := b.store.ActivityLock.
				ExistsByKey(instanceID, item.Event.ID).
				Exec(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to check for lock: %w", err)
			}
			if locked {
				continue
			}
			err = b.store.ActivityLock.
				Create(&activity_lock.Value{
					WorkflowInstanceID: instanceID,
					EventID:            item.Event.ID,
					CreatedAt:          time.Now(),
				}).
				Exec(ctx)
			if err != nil {
				if errors.Is(err, storage.ErrAlreadyExists) {
					// Another worker managed to lock this first
					continue
				}
				return nil, fmt.Errorf("failed to lock activity: %w", err)
			}
			return &backend.ActivityTask{
				ID:               item.Event.ID,
				ActivityID:       item.Event.ID,
				Queue:            core.Queue(item.Queue),
				WorkflowInstance: core.NewWorkflowInstance(instanceID, executionID),
				Event:            item.Event,
			}, nil
		}
	}

	return nil, nil
}

func (b *Backend) ExtendActivityTask(ctx context.Context, task *backend.ActivityTask) error {
	lock, err := b.store.ActivityLock.
		GetByKey(task.WorkflowInstance.InstanceID, task.Event.ID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to get existing activity lock: %w", err)
	}
	err = b.store.ActivityLock.
		Update(lock).
		WithTTL(b.options.ActivityLockTimeout).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update activity lock: %w", err)
	}
	return nil
}

func (b *Backend) CompleteActivityTask(ctx context.Context, task *backend.ActivityTask, result *history.Event) error {
	err := b.store.Txn(
		b.store.ActivityLock.DeleteByKey(
			task.WorkflowInstance.InstanceID,
			task.Event.ID,
		),
		b.store.ActivityQueueItem.DeleteByKey(
			string(task.Queue),
			task.WorkflowInstance.InstanceID,
			task.Event.ID,
		),
		b.store.PendingEvent.Create(&pending_event.Value{
			WorkflowInstanceID:  task.WorkflowInstance.InstanceID,
			WorkflowExecutionID: task.WorkflowInstance.ExecutionID,
			Event:               result,
		}),
	).Commit(ctx)
	if err != nil {
		return fmt.Errorf("failed to complete persist activity completion: %w", err)
	}
	return nil
}

func (b *Backend) GetStats(ctx context.Context) (*backend.Stats, error) {
	now := time.Now()
	instances, err := b.store.WorkflowQueueItem.
		GetAll().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get queued workflow instances: %w", err)
	}
	// Every workflow in the queue is active
	activeWorkflowInstances := int64(len(instances))
	pendingWorkflowTasks := map[core.Queue]int64{}
	for _, instance := range instances {
		locked, err := b.store.WorkflowInstanceLock.
			ExistsByKey(instance.WorkflowInstance.InstanceID, instance.WorkflowInstance.ExecutionID).
			Exec(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to check workflow instance lock: %w", err)
		}
		if locked {
			continue
		}
		events, err := b.store.PendingEvent.
			GetByInstanceExecution(
				instance.WorkflowInstance.InstanceID,
				instance.WorkflowInstance.ExecutionID,
			).
			Exec(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get pending events: %w", err)
		}
		// If it has at least one visible pending event, it's ready to be picked
		// up.
		for _, event := range events {
			if event.Event.VisibleAt == nil || event.Event.VisibleAt.Before(now) {
				pendingWorkflowTasks[instance.Queue] += 1
				break
			}
		}
	}
	activities, err := b.store.ActivityQueueItem.
		GetAll().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get queued activities: %w", err)
	}
	pendingActivityTasks := map[core.Queue]int64{}
	for _, act := range activities {
		pendingActivityTasks[core.Queue(act.Queue)] += 1
	}
	return &backend.Stats{
		ActiveWorkflowInstances: activeWorkflowInstances,
		PendingActivityTasks:    pendingActivityTasks,
		PendingWorkflowTasks:    pendingWorkflowTasks,
	}, nil
}

func (b *Backend) Tracer() trace.Tracer {
	return b.options.TracerProvider.Tracer("workflows-etcd-backend")
}

func (b *Backend) Metrics() metrics.Client {
	return b.options.Metrics
}

func (b *Backend) Options() *backend.Options {
	return b.options
}

func (b *Backend) Close() error {
	return nil
}

func (b *Backend) FeatureSupported(feature backend.Feature) bool {
	switch feature {
	case backend.Feature_Expiration:
		return true
	}
	return false
}
