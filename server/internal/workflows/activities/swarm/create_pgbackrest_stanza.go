package swarm

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/swarm"
)

type CreatePgBackRestStanzaInput struct {
	Instance *database.InstanceSpec `json:"instance"`
}

func (i *CreatePgBackRestStanzaInput) Validate() error {
	var errs []error
	if i.Instance == nil {
		errs = append(errs, errors.New("instance: must be specified"))
	}
	if i.Instance.BackupConfig == nil {
		errs = append(errs, errors.New("instance.backup_config: must be specified"))
	} else if i.Instance.BackupConfig.Provider != database.BackupProviderPgBackrest {
		errs = append(errs, errors.New("instance.backup_config.provider: must be pgbackrest for this activity"))
	}
	return errors.Join(errs...)
}

type CreatePgBackRestStanzaOutput struct {
	CreateStanzaOutput string
	CheckOutput        string
}

func (a *Activities) ExecuteCreatePgBackRestStanza(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *CreatePgBackRestStanzaInput,
) workflow.Future[*CreatePgBackRestStanzaOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(hostID.String()),
	}
	return workflow.ExecuteActivity[*CreatePgBackRestStanzaOutput](ctx, options, a.CreatePgBackRestStanza, input)
}

func (a *Activities) CreatePgBackRestStanza(ctx context.Context, input *CreatePgBackRestStanzaInput) (*CreatePgBackRestStanzaOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	stanzaCreate := pgbackrestBackupCmd("stanza-create", "--io-timeout", "10s").StringSlice()
	stanzaCreateOutput, err := swarm.PostgresContainerExec(ctx, a.Docker, input.Instance.InstanceID, stanzaCreate)
	if err != nil {
		return nil, fmt.Errorf("error during stanza-create: %w", err)
	}
	check := pgbackrestBackupCmd("check").StringSlice()
	checkOutput, err := swarm.PostgresContainerExec(ctx, a.Docker, input.Instance.InstanceID, check)
	if err != nil {
		return nil, fmt.Errorf("error during check: %w", err)
	}
	return &CreatePgBackRestStanzaOutput{
		CreateStanzaOutput: stanzaCreateOutput,
		CheckOutput:        checkOutput,
	}, nil
}
