package general

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
)

type CreateDataDirInput struct {
	DataDir string `json:"data_dir"`
	Owner   Owner  `json:"owner,omitempty"`
}

func (i *CreateDataDirInput) Validate() error {
	var errs []error
	if i.DataDir == "" {
		errs = append(errs, errors.New("data_dir: cannot be empty"))
	}
	return errors.Join(errs...)
}

type CreateDataDirOutput struct {
	DataDir string `json:"data_dir"`
}

func (a *Activities) ExecuteCreateDataDir(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *CreateDataDirInput,
) workflow.Future[*CreateDataDirOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(hostID.String()),
	}
	return workflow.ExecuteActivity[*CreateDataDirOutput](ctx, options, a.CreateDataDir, input)
}

func (a *Activities) CreateDataDir(ctx context.Context, input *CreateDataDirInput) (*CreateDataDirOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	// paths := HostPathsFor(cfg, input.Spec)

	// path := filepath.Join(cfg.DataDir, input.Name)
	if err := a.Fs.MkdirAll(input.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to make directory: %w", err)
	}

	// This is safe to run every time because Owner.String() returns ":" if
	// neither group nor user are specified.
	if _, err := a.Run(ctx, "sudo", "chown", "-R", input.Owner.String(), input.DataDir); err != nil {
		return nil, fmt.Errorf("failed to change mount path ownership: %w", err)
	}

	return &CreateDataDirOutput{
		DataDir: input.DataDir,
	}, nil
}
