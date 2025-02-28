package general

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/filesystem"
)

type CreateLoopDeviceInput struct {
	DataDir  string `json:"data_dir"`
	SizeSpec string `json:"size_spec"`
	Owner    Owner  `json:"owner,omitempty"`
}

func (i *CreateLoopDeviceInput) Validate() error {
	var errs []error
	if i.DataDir == "" {
		errs = append(errs, errors.New("data_dir: cannot be empty"))
	}
	if i.SizeSpec == "" {
		errs = append(errs, errors.New("size_spec: cannot be empty"))
	}
	return errors.Join(errs...)
}

type CreateLoopDeviceOutput struct {
	DataDir string `json:"data_dir"`
}

func (a *Activities) ExecuteCreateLoopDevice(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *CreateLoopDeviceInput,
) workflow.Future[*CreateLoopDeviceOutput] {
	opts := workflow.ActivityOptions{
		Queue: core.Queue(hostID.String()),
	}
	return workflow.ExecuteActivity[*CreateLoopDeviceOutput](ctx, opts, a.CreateLoopDevice, input)
}

func (a *Activities) CreateLoopDevice(ctx context.Context, input *CreateLoopDeviceInput) (*CreateLoopDeviceOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	opts := filesystem.MakeLoopDeviceOptions{
		SizeSpec:  input.SizeSpec,
		MountPath: input.DataDir,
		Owner: filesystem.Owner{
			User:  input.Owner.User,
			Group: input.Owner.Group,
		},
	}

	if err := a.LoopMgr.MakeLoopDevice(ctx, opts); err != nil {
		return nil, fmt.Errorf("failed to make loop device: %w", err)
	}

	return &CreateLoopDeviceOutput{
		DataDir: input.DataDir,
	}, nil
}
