package swarm

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/docker/docker/api/types/network"
	"github.com/google/uuid"
)

type CreateDBNetworksInput struct {
	DatabaseID uuid.UUID `json:"database_id"`
}

func (i *CreateDBNetworksInput) Validate() error {
	var errs []error
	if i.DatabaseID == uuid.Nil {
		errs = append(errs, errors.New("database_id: cannot be empty"))
	}
	return errors.Join(errs...)
}

type CreateDBNetworksOutput struct {
	// BridgeNetwork   NetworkInfo `json:"bridge_network"`
	DatabaseNetwork NetworkInfo `json:"database_network"`
}

func (a *Activities) ExecuteCreateDBNetworks(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *CreateDBNetworksInput,
) workflow.Future[*CreateDBNetworksOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(hostID.String()),
	}
	return workflow.ExecuteActivity[*CreateDBNetworksOutput](ctx, options, a.CreateDBNetworks, input)
}

func (a *Activities) CreateDBNetworks(ctx context.Context, input *CreateDBNetworksInput) (*CreateDBNetworksOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	// TODO: It's not clear what the behavior should be if the bridge network
	// already exists, because we're not able to recover the subnet and gateway
	// without inspecting an existing config network. We're just going to fail
	// if the bridge network already exists for now, and do the same for the
	// overlay network in order to keep things simple.

	// var bridgeInfo NetworkInfo
	var databaseInfo NetworkInfo

	// Bridge networks are local to the host, so we can't set the subnet and
	// gateway on the swarm-scoped network. Instead, each host that runs an
	// instance of this database will need a config network that specifies the
	// subnet and gateway.
	// bridgeConfigName := fmt.Sprintf("%s_bridge_config", input.DatabaseID)
	// bridge := fmt.Sprintf("%s_bridge", input.DatabaseID)
	// bridgeSubnet, err := a.IPAM.AllocateBridgeSubnet(ctx)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to allocate bridge subnet: %w", err)
	// }
	// bridgeNetworkID, err := a.Docker.NetworkCreate(ctx, bridge, network.CreateOptions{
	// 	Scope:  "swarm",
	// 	Driver: "bridge",
	// 	ConfigFrom: &network.ConfigReference{
	// 		Network: bridgeConfigName,
	// 	},
	// })
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to create bridge network: %w", err)
	// }
	// bridgeInfo = NetworkInfo{
	// 	Name:    bridge,
	// 	ID:      bridgeNetworkID,
	// 	Subnet:  bridgeSubnet,
	// 	Gateway: bridgeSubnet.Addr().Next(),
	// }

	database := fmt.Sprintf("%s_database", input.DatabaseID)
	databaseSubnet, err := a.IPAM.AllocateDatabaseSubnet(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate bridge subnet: %w", err)
	}
	databaseGateway := databaseSubnet.Addr().Next()
	databaseNetworkID, err := a.Docker.NetworkCreate(ctx, database, network.CreateOptions{
		Scope:  "swarm",
		Driver: "overlay",
		IPAM: &network.IPAM{
			Config: []network.IPAMConfig{
				{
					Subnet:  databaseSubnet.String(),
					Gateway: databaseGateway.String(),
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create bridge network: %w", err)
	}
	databaseInfo = NetworkInfo{
		Name:    database,
		ID:      databaseNetworkID,
		Subnet:  databaseSubnet,
		Gateway: databaseGateway,
	}

	return &CreateDBNetworksOutput{
		// BridgeNetwork:   bridgeInfo,
		DatabaseNetwork: databaseInfo,
	}, nil
}
