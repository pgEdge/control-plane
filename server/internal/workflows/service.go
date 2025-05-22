package workflows

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/backend"
	"github.com/cschleiden/go-workflows/client"
	"github.com/cschleiden/go-workflows/core"
	"github.com/pgEdge/control-plane/server/internal/config"
)

var ErrDuplicateWorkflow = errors.New("duplicate workflow already in progress")

type Service struct {
	cfg       config.Config
	client    *client.Client
	workflows *Workflows
}

func NewService(
	cfg config.Config,
	client *client.Client,
	workflows *Workflows,
) *Service {
	return &Service{
		cfg:       cfg,
		client:    client,
		workflows: workflows,
	}
}

func (s *Service) UpdateDatabase(ctx context.Context, input *UpdateDatabaseInput) error {
	databaseID := input.Spec.DatabaseID
	opts := client.WorkflowInstanceOptions{
		Queue:      core.Queue(s.cfg.HostID.String()),
		InstanceID: databaseID.String(), // Using a stable ID functions as a locking mechanism
	}
	_, err := s.client.CreateWorkflowInstance(ctx, opts, s.workflows.UpdateDatabase, input)
	if err != nil {
		return s.translateCreateErr(err)
	}

	return nil
}

func (s *Service) DeleteDatabase(ctx context.Context, input *DeleteDatabaseInput) error {
	databaseID := input.DatabaseID
	opts := client.WorkflowInstanceOptions{
		Queue:      core.Queue(s.cfg.HostID.String()),
		InstanceID: databaseID.String(),
	}
	_, err := s.client.CreateWorkflowInstance(ctx, opts, s.workflows.DeleteDatabase, input)
	if err != nil {
		return s.translateCreateErr(err)
	}

	return nil
}

func (s *Service) CreatePgBackRestBackup(ctx context.Context, input *CreatePgBackRestBackupInput) error {
	databaseID := input.DatabaseID
	nodeName := input.NodeName
	opts := client.WorkflowInstanceOptions{
		Queue:      core.Queue(s.cfg.HostID.String()),
		InstanceID: databaseID.String() + "-" + nodeName,
	}
	_, err := s.client.CreateWorkflowInstance(ctx, opts, s.workflows.CreatePgBackRestBackup, input)
	if err != nil {
		return s.translateCreateErr(err)
	}

	return nil
}

func (s *Service) PgBackRestRestore(ctx context.Context, input *PgBackRestRestoreInput) error {
	databaseID := input.Spec.DatabaseID
	opts := client.WorkflowInstanceOptions{
		Queue:      core.Queue(s.cfg.HostID.String()),
		InstanceID: databaseID.String(),
	}
	_, err := s.client.CreateWorkflowInstance(ctx, opts, s.workflows.PgBackRestRestore, input)
	if err != nil {
		return s.translateCreateErr(err)
	}

	return nil
}

func (s *Service) translateCreateErr(err error) error {
	if errors.Is(err, backend.ErrInstanceAlreadyExists) {
		return ErrDuplicateWorkflow
	}
	return fmt.Errorf("failed to create workflow instance: %w", err)
}
