package swarm

import (
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/workflows/activities/general"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities/swarm"
)

type CreateCohortInstancesInput struct {
	Partition   *general.CohortPartition
	ClusterSize int
}

type CreateCohortInstancesOutput struct{}

func (w *Workflows) ExecuteCreateCohortInstances(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *CreateCohortInstancesInput,
) workflow.Future[*CreateCohortInstancesOutput] {
	options := workflow.SubWorkflowOptions{
		Queue: core.Queue(hostID.String()),
	}
	return workflow.CreateSubWorkflowInstance[*CreateCohortInstancesOutput](ctx, options, w.CreateCohortInstances, input)
}

func (w *Workflows) CreateCohortInstances(ctx workflow.Context, input *CreateCohortInstancesInput) (*CreateCohortInstancesOutput, error) {
	cohortID := input.Partition.Cohort.CohortID
	managerID := input.Partition.Cohort.Manager.ID
	logger := workflow.Logger(ctx).With("cohort_id", cohortID)
	logger.Info("creating cohort instances",
		"num_hosts", len(input.Partition.Hosts),
		"manager_id", managerID.String(),
	)

	networks, err := w.Swarm.ExecuteCreateDBNetworks(ctx, managerID, &swarm.CreateDBNetworksInput{
		DatabaseID: input.Partition.Database.DatabaseID,
	}).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create database networks: %w", err)
	}

	var futures []workflow.Future[*CreateHostInstancesOutput]
	for _, hostPartition := range input.Partition.Hosts {
		futures = append(futures, w.ExecuteCreateHostInstances(ctx, hostPartition.Host.ID, &CreateHostInstancesInput{
			Manager:         input.Partition.Cohort.Manager,
			Partition:       hostPartition,
			DatabaseNetwork: networks.DatabaseNetwork,
			ClusterSize:     input.ClusterSize,
		}))
	}

	var errs []error
	for _, future := range futures {
		if _, err := future.Get(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to create host instances: %w", err))
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	logger.Info("successfully created cohort instances")

	return &CreateCohortInstancesOutput{}, nil

	// managerOptions := workflow.ActivityOptions{
	// 	Queue: core.Queue(input.Partition.Cohort.Manager.ID.String()),
	// }
	// networks, err := workflow.ExecuteActivity[*swarm.CreateDBNetworksOutput](ctx,
	// 	managerOptions,
	// 	w.Swarm.CreateDBNetworks,
	// 	&swarm.CreateDBNetworksInput{
	// 		DatabaseID: input.Partition.Database.DatabaseID,
	// 		BridgeConfig: ,
	// 	},
	// ).Get(ctx)
}
