package etcd

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/diag"
)

var _ diag.Backend = (*Backend)(nil)

func (b *Backend) GetWorkflowInstance(ctx context.Context, instance *core.WorkflowInstance) (*diag.WorkflowInstanceRef, error) {
	inst, err := b.store.WorkflowInstance.
		GetByKey(instance.InstanceID, instance.ExecutionID).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow instance: %w", err)
	}

	return &diag.WorkflowInstanceRef{
		Instance:    inst.WorkflowInstance,
		CreatedAt:   inst.CreatedAt,
		CompletedAt: inst.FinishedAt,
		State:       inst.State,
		Queue:       string(inst.Queue),
	}, nil
}

func (b *Backend) GetWorkflowInstances(ctx context.Context, afterInstanceID, afterExecutionID string, count int) ([]*diag.WorkflowInstanceRef, error) {
	instances, err := b.store.WorkflowInstance.
		GetAll().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get all workflow instances: %w", err)
	}
	var start int
	if afterInstanceID != "" {
		for idx, instance := range instances {
			if instance.WorkflowInstance.InstanceID == afterInstanceID &&
				instance.WorkflowInstance.ExecutionID == afterExecutionID {
				start = idx + 1
				break
			}
		}
	}
	if start > len(instances)-1 {
		return nil, nil
	}

	end := min(start+count, len(instances))

	var refs []*diag.WorkflowInstanceRef
	for _, instance := range instances[start:end] {
		refs = append(refs, &diag.WorkflowInstanceRef{
			Instance:    instance.WorkflowInstance,
			CreatedAt:   instance.CreatedAt,
			CompletedAt: instance.FinishedAt,
			State:       instance.State,
			Queue:       string(instance.Queue),
		})
	}
	return refs, nil
}

func (b *Backend) GetWorkflowTree(ctx context.Context, instance *core.WorkflowInstance) (*diag.WorkflowInstanceTree, error) {
	itb := diag.NewInstanceTreeBuilder(b)
	return itb.BuildWorkflowInstanceTree(ctx, instance)
}
