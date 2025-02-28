package swarm

import (
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities/general"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities/swarm"
)

type CreateHostInstancesInput struct {
	Manager   *host.Host
	Partition *general.HostPartition
	// BridgeNetwork   swarm.NetworkInfo
	DatabaseNetwork swarm.NetworkInfo
	ClusterSize     int
}

// TODO: should have information about the instances created
type CreateHostInstancesOutput struct{}

func (w *Workflows) ExecuteCreateHostInstances(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *CreateHostInstancesInput,
) workflow.Future[*CreateHostInstancesOutput] {
	options := workflow.SubWorkflowOptions{
		Queue: core.Queue(hostID.String()),
	}
	return workflow.CreateSubWorkflowInstance[*CreateHostInstancesOutput](ctx, options, w.CreateHostInstances, input)
}

func (w *Workflows) CreateHostInstances(ctx workflow.Context, input *CreateHostInstancesInput) (*CreateHostInstancesOutput, error) {
	logger := workflow.Logger(ctx).With("host_id", input.Partition.Host.ID.String())
	logger.Info("creating host instances",
		"num_instances", len(input.Partition.Instances))

	for _, instance := range input.Partition.Instances {
		_, err := w.ExecuteCreateInstance(ctx, input.Partition.Host.ID, &CreateInstanceInput{
			Host:     input.Partition.Host,
			Manager:  input.Manager,
			Instance: instance,
			// BridgeNetwork:   input.BridgeNetwork,
			DatabaseNetwork: input.DatabaseNetwork,
			ClusterSize:     input.ClusterSize,
		}).Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create instance: %w", err)
		}
	}

	logger.Info("successfully created host instances")

	return &CreateHostInstancesOutput{}, nil
	// networks, err := w.Swarm.ExecuteCreateDBNetworks(ctx, managerID, &swarm.CreateDBNetworksInput{
	// 	DatabaseID: input.Partition.Database.DatabaseID,
	// }).Get(ctx)
	// if err != nil {
	// 	return fmt.Errorf("failed to create database networks: %w", err)
	// }

	// for _, hostPartition := range input.Partition.Hosts {
	// 	hostPartition.Instances
	// }

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
