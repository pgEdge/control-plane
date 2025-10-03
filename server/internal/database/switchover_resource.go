package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*SwitchoverResource)(nil)

const ResourceTypeSwitchover resource.Type = "database.switchover"

func SwitchoverResourceIdentifier(nodeName string) resource.Identifier {
	return resource.Identifier{
		ID:   nodeName,
		Type: ResourceTypeSwitchover,
	}
}

type SwitchoverResource struct {
	HostID     string               `json:"host_id"`
	InstanceID string               `json:"instance_id"`
	TargetRole patroni.InstanceRole `json:"target_role"`
}

func (s *SwitchoverResource) ResourceVersion() string {
	return "1"
}

func (s *SwitchoverResource) DiffIgnore() []string {
	return nil
}

func (s *SwitchoverResource) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeHost,
		ID:   s.HostID,
	}
}

func (s *SwitchoverResource) Identifier() resource.Identifier {
	return SwitchoverResourceIdentifier(s.InstanceID)
}

func (s *SwitchoverResource) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		InstanceResourceIdentifier(s.InstanceID),
	}
}

func (s *SwitchoverResource) Refresh(ctx context.Context, rc *resource.Context) error {
	if !rc.State.HasResources(s.Dependencies()...) {
		return resource.ErrNotFound
	}
	return nil
}

func (s *SwitchoverResource) Create(ctx context.Context, rc *resource.Context) error {
	logger, err := do.Invoke[zerolog.Logger](rc.Injector)
	if err != nil {
		return err
	}

	instance, err := resource.FromContext[*InstanceResource](rc, InstanceResourceIdentifier(s.InstanceID))
	if err != nil {
		return fmt.Errorf("failed to retrieve instance from state: %w", err)
	}

	patroniClient := patroni.NewClient(instance.ConnectionInfo.PatroniURL(), nil)

	switch s.TargetRole {
	case "", patroni.InstanceRolePrimary:
		return s.switchoverToInstance(ctx, patroniClient, logger)
	case patroni.InstanceRoleReplica:
		return s.switchoverToPeer(ctx, patroniClient, logger)
	default:
		return fmt.Errorf("unrecognized target role '%s'", s.TargetRole)
	}
}

func (s *SwitchoverResource) Update(ctx context.Context, rc *resource.Context) error {
	return s.Create(ctx, rc)
}

func (s *SwitchoverResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (s *SwitchoverResource) isPrimary(ctx context.Context, patroniClient *patroni.Client) (bool, error) {
	status, err := patroniClient.GetInstanceStatus(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get patroni instance status: %w", err)
	}

	return status.IsPrimary(), nil
}

func (s *SwitchoverResource) switchover(
	ctx context.Context,
	patroniClient *patroni.Client,
	logger zerolog.Logger,
	target string,
) error {
	cluster, err := patroniClient.GetClusterStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to get patroni cluster status: %w", err)
	}

	var candidate *string
	if target != "" {
		candidate = &target
	} else if r, ok := cluster.MostAlignedReplica(); ok {
		candidate = r.Name
	} else {
		logger.Warn().
			Str("instance_id", s.InstanceID).
			Msg("skipping switchover - no viable candidates found")

		return nil
	}

	leader, ok := cluster.Leader()
	if !ok {
		return fmt.Errorf("patroni cluster has no leader")
	}
	if leader.Name == nil {
		return errors.New("cluster leader name undefined")
	}

	logger.Info().
		Str("leader", *leader.Name).
		Str("candidate", *candidate).
		Msg("performing switchover from leader to candidate")

	opts := &patroni.Switchover{
		Leader:    leader.Name,
		Candidate: candidate,
	}
	err = patroniClient.ScheduleSwitchover(ctx, opts, true)
	if err != nil {
		return fmt.Errorf("failed to perform switchover to peer: %w", err)
	}

	err = WaitForPatroniRunning(ctx, patroniClient, time.Minute)
	if err != nil {
		return fmt.Errorf("failed while waiting for patroni to report running state: %w", err)
	}

	return nil
}

func (s *SwitchoverResource) switchoverToPeer(
	ctx context.Context,
	patroniClient *patroni.Client,
	logger zerolog.Logger,
) error {
	isPrimary, err := s.isPrimary(ctx, patroniClient)
	if err != nil {
		return err
	}
	if !isPrimary {
		return nil
	}

	return s.switchover(ctx, patroniClient, logger, "")
}

func (s *SwitchoverResource) switchoverToInstance(
	ctx context.Context,
	patroniClient *patroni.Client,
	logger zerolog.Logger,
) error {
	isPrimary, err := s.isPrimary(ctx, patroniClient)
	if err != nil {
		return err
	}
	if isPrimary {
		return nil
	}

	return s.switchover(ctx, patroniClient, logger, s.InstanceID)
}
