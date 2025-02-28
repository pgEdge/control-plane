package swarm

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/docker/docker/api/types/network"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/docker"
)

// const SwarmCreateBridgeConfig = "SwarmCreateBridgeConfig"

type CreateBridgeConfigInput struct {
	DatabaseID uuid.UUID    `json:"database_id"`
	Subnet     netip.Prefix `json:"subnet"`
	Gateway    netip.Addr   `json:"gateway"`
}

func (i *CreateBridgeConfigInput) Validate() error {
	var errs []error
	if i.DatabaseID == uuid.Nil {
		errs = append(errs, errors.New("database_id: cannot be empty"))
	}
	return errors.Join(errs...)
}

type CreateBridgeConfigOutput struct {
	BridgeConfig NetworkInfo `json:"bridge_config"`
}

func (a *Activities) ExecuteCreateBridgeConfig(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *CreateBridgeConfigInput,
) workflow.Future[*CreateBridgeConfigOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(hostID.String()),
	}
	return workflow.ExecuteActivity[*CreateBridgeConfigOutput](ctx, options, a.CreateBridgeConfig, input)
}

func (a *Activities) CreateBridgeConfig(ctx context.Context, input *CreateBridgeConfigInput) (*CreateBridgeConfigOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	name := fmt.Sprintf("%s_bridge_config", input.DatabaseID)
	inspect, err := a.Docker.NetworkInspect(ctx, name, network.InspectOptions{
		Scope: "local",
	})
	if err == nil {
		info, err := docker.ExtractNetworkInfo(inspect)
		if err != nil {
			return nil, fmt.Errorf("failed to extract network info for bridge config %q: %w", name, err)
		}
		if info.Subnet.String() != input.Subnet.String() || info.Gateway.String() != input.Gateway.String() {
			return nil, errors.New("existing bridge config does not match input")
		}
		return &CreateBridgeConfigOutput{
			BridgeConfig: NetworkInfo{
				Name:    name,
				ID:      info.ID,
				Subnet:  info.Subnet,
				Gateway: info.Gateway,
			},
		}, nil
	} else if !errors.Is(err, docker.ErrNotFound) {
		return nil, fmt.Errorf("failed to check for existing network: %w", err)
	}

	// subnet, err := service.AllocateBridgeSubnet(ctx)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to allocate bridge subnet: %w", err)
	// }
	// gateway := subnet.Addr().Next()

	id, err := a.Docker.NetworkCreate(ctx, name, network.CreateOptions{
		ConfigOnly: true,
		IPAM: &network.IPAM{
			Config: []network.IPAMConfig{
				{
					Subnet:  input.Subnet.String(),
					Gateway: input.Gateway.String(),
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create bridge network config: %w", err)
	}

	return &CreateBridgeConfigOutput{
		BridgeConfig: NetworkInfo{
			Name:    name,
			ID:      id,
			Subnet:  input.Subnet,
			Gateway: input.Gateway,
		},
	}, nil
}
