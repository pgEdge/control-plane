package activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type RemoveHostFromEtcdInput struct {
	HostID string `json:"host_id"`
}

type RemoveHostFromEtcdOutput struct{}

func (a *Activities) ExecuteRemoveHostFromEtcd(
	ctx workflow.Context,
	input *RemoveHostFromEtcdInput,
) workflow.Future[*RemoveHostFromEtcdOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*RemoveHostFromEtcdOutput](ctx, options, a.RemoveHostFromEtcd, input)
}

// RemoveHostFromEtcd removes the host's Etcd cluster membership, if any, and
// its Etcd credentials.
func (a *Activities) RemoveHostFromEtcd(ctx context.Context, input *RemoveHostFromEtcdInput) (*RemoveHostFromEtcdOutput, error) {
	logger := activity.Logger(ctx)
	if input == nil {
		return nil, errors.New("input is nil")
	}
	logger = logger.With(
		"host_id", input.HostID,
	)
	logger.Info("starting remove host from etcd activity")

	etcdClient, err := do.Invoke[etcd.Etcd](a.Injector)
	if err != nil {
		return nil, fmt.Errorf("failed to get etcd client: %w", err)
	}

	err = etcdClient.RemoveHost(ctx, input.HostID)
	if err != nil {
		return nil, fmt.Errorf("failed to remove host from etcd: %w", err)
	}

	logger.Info("remove host from etcd activity completed")
	return &RemoveHostFromEtcdOutput{}, nil
}

type RemoveHostInput struct {
	HostID string `json:"host_id"`
}

type RemoveHostOutput struct{}

func (a *Activities) ExecuteRemoveHost(
	ctx workflow.Context,
	input *RemoveHostInput,
) workflow.Future[*RemoveHostOutput] {
	options := workflow.ActivityOptions{
		Queue: utils.HostQueue(a.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*RemoveHostOutput](ctx, options, a.RemoveHost, input)
}

// RemoveHost removes the host's record from the host service. The host must
// already have been removed from Etcd via RemoveHostFromEtcd.
func (a *Activities) RemoveHost(ctx context.Context, input *RemoveHostInput) (*RemoveHostOutput, error) {
	logger := activity.Logger(ctx)
	if input == nil {
		return nil, errors.New("input is nil")
	}
	logger = logger.With(
		"host_id", input.HostID,
	)
	logger.Info("starting remove host activity")

	hostSvc, err := do.Invoke[*host.Service](a.Injector)
	if err != nil {
		return nil, fmt.Errorf("failed to get host service: %w", err)
	}

	err = hostSvc.RemoveHost(ctx, input.HostID)
	if err != nil {
		return nil, fmt.Errorf("failed to remove host from host service: %w", err)
	}

	logger.Info("remove host activity completed")
	return &RemoveHostOutput{}, nil
}
