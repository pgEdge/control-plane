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

func (a *Activities) RemoveHost(ctx context.Context, input *RemoveHostInput) (*RemoveHostOutput, error) {
	logger := activity.Logger(ctx)
	if input == nil {
		return nil, errors.New("input is nil")
	}
	logger = logger.With(
		"host_id", input.HostID,
	)
	logger.Info("starting remove host activity")

	etcdClient, err := do.Invoke[etcd.Etcd](a.Injector)
	if err != nil {
		return nil, fmt.Errorf("failed to get etcd client: %w", err)
	}

	hostSvc, err := do.Invoke[*host.Service](a.Injector)
	if err != nil {
		return nil, fmt.Errorf("failed to get host service: %w", err)
	}

	err = etcdClient.RemoveHost(ctx, input.HostID)
	if err != nil {
		return nil, fmt.Errorf("failed to remove host from etcd: %w", err)
	}

	err = hostSvc.RemoveHost(ctx, input.HostID)
	if err != nil {
		return nil, fmt.Errorf("failed to remove host from host service: %w", err)
	}

	logger.Info("remove host activity completed")
	return &RemoveHostOutput{}, nil
}
