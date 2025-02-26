package swarm

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/docker/docker/api/types/swarm"
	"github.com/google/uuid"
)

// const SwarmCreateDBService = "SwarmCreateDBService"

type CreateDBServiceInput struct {
	Service swarm.ServiceSpec
}

func (i *CreateDBServiceInput) Validate() error {
	var errs []error
	// if i.DatabaseID == "" {
	// 	errs = append(errs, errors.New("database_id: cannot be empty"))
	// }
	// if i.SizeSpec == "" {
	// 	errs = append(errs, errors.New("size_spec: cannot be empty"))
	// }
	return errors.Join(errs...)
}

type CreateDBServiceOutput struct {
	ServiceID string `json:"service_id"`
}

func (a *Activities) ExecuteCreateDBService(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *CreateDBServiceInput,
) workflow.Future[*CreateDBServiceOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(hostID.String()),
	}
	return workflow.ExecuteActivity[*CreateDBServiceOutput](ctx, options, a.CreateDBService, input)
}

func (a *Activities) CreateDBService(ctx context.Context, input *CreateDBServiceInput) (*CreateDBServiceOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	svcID, err := a.Docker.ServiceDeploy(ctx, input.Service)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy service: %w", err)
	}

	if err := a.Docker.WaitForService(ctx, svcID, 0); err != nil {
		return nil, fmt.Errorf("failed to wait for service: %w", err)
	}

	return &CreateDBServiceOutput{
		ServiceID: svcID,
	}, nil
}
