package swarm

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	docker "github.com/docker/docker/api/types/swarm"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/swarm"
)

// const SwarmCreateDBService = "SwarmCreateDBService"

type GetServiceSpecInput struct {
	Instance *database.InstanceSpec `json:"spec"`
	// BridgeNetwork   NetworkInfo            `json:"bridge_network"`
	DatabaseNetwork NetworkInfo `json:"database_network"`
}

func (i *GetServiceSpecInput) Validate() error {
	var errs []error
	// if i.DatabaseID == "" {
	// 	errs = append(errs, errors.New("database_id: cannot be empty"))
	// }
	// if i.SizeSpec == "" {
	// 	errs = append(errs, errors.New("size_spec: cannot be empty"))
	// }
	return errors.Join(errs...)
}

type GetServiceSpecOutput struct {
	Service docker.ServiceSpec
}

func (a *Activities) ExecuteGetServiceSpec(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *GetServiceSpecInput,
) workflow.Future[*GetServiceSpecOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(hostID.String()),
	}
	return workflow.ExecuteActivity[*GetServiceSpecOutput](ctx, options, a.GetServiceSpec, input)
}

func (a *Activities) GetServiceSpec(ctx context.Context, input *GetServiceSpecInput) (*GetServiceSpecOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	host, err := a.HostService.GetHost(ctx, a.Config.HostID)
	if err != nil {
		return nil, fmt.Errorf("failed to get host: %w", err)
	}

	paths := HostPathsFor(a.Config, input.Instance)
	spec, err := swarm.DatabaseServiceSpec(host, a.Config, input.Instance, &swarm.HostOptions{
		DatabaseNetworkID: input.DatabaseNetwork.ID,
		Paths: swarm.Paths{
			Configs:      paths.Configs.Dir,
			Certificates: paths.Certificates.Dir,
			Data:         paths.Data.Dir,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate service spec: %w", err)
	}

	return &GetServiceSpecOutput{
		Service: spec,
	}, nil
}
