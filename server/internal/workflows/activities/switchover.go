package activities

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/activity"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/monitor"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type InstanceHost struct {
	InstanceID string `json:"instance_id"`
	HostID     string `json:"host_id"`
}
type SelectSwitchoverCandidateInput struct {
	DatabaseID      string          `json:"database_id"`
	NodeName        string          `json:"node_name"`
	ExcludeInstance string          `json:"exclude_instance"`
	Instances       []*InstanceHost `json:"instances,omitempty"` // optional
}

type SelectSwitchoverCandidateOutput struct {
	CandidateInstanceID string `json:"candidate_instance_id"`
	CandidateHostID     string `json:"candidate_host_id,omitempty"`
}

func (a *Activities) ExecuteSelectSwitchoverCandidate(ctx workflow.Context, input *SelectSwitchoverCandidateInput) workflow.Future[*SelectSwitchoverCandidateOutput] {
	opts := workflow.ActivityOptions{
		Queue: utils.ClusterQueue(),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*SelectSwitchoverCandidateOutput](ctx, opts, a.SelectSwitchoverCandidate, input)
}

func (a *Activities) SelectSwitchoverCandidate(ctx context.Context, input *SelectSwitchoverCandidateInput) (*SelectSwitchoverCandidateOutput, error) {
	if input == nil {
		return nil, fmt.Errorf("SelectSwitchoverCandidate: input is nil")
	}

	makeOutput := func(inst *InstanceHost) *SelectSwitchoverCandidateOutput {
		if inst == nil {
			return nil
		}
		return &SelectSwitchoverCandidateOutput{
			CandidateInstanceID: inst.InstanceID,
			CandidateHostID:     inst.HostID,
		}
	}

	monitorStore, err := do.Invoke[*monitor.Store](a.Injector)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize monitor service: %w", err)
	}

	if len(input.Instances) > 0 {
		if monitorStore.InstanceMonitor != nil {
			for _, inst := range input.Instances {
				if inst == nil || inst.InstanceID == input.ExcludeInstance {
					continue
				}

				stored, err := monitorStore.InstanceMonitor.GetByKey(inst.HostID, inst.InstanceID).Exec(ctx)
				if err == nil && stored != nil {
					return makeOutput(inst), nil
				}
			}
		}

		for _, inst := range input.Instances {
			if inst == nil || inst.InstanceID == input.ExcludeInstance {
				continue
			}
			return makeOutput(inst), nil
		}
	}

	return nil, fmt.Errorf("SelectSwitchoverCandidate: no eligible candidate found for database=%s node=%s", input.DatabaseID, input.NodeName)
}

type PerformSwitchoverInput struct {
	DatabaseID          string    `json:"database_id"`
	LeaderInstanceID    string    `json:"leader_instance_id"`
	CandidateInstanceID string    `json:"candidate_instance_id,omitempty"`
	ScheduledAt         time.Time `json:"scheduled_at,omitempty"`
	TaskID              uuid.UUID `json:"task_id"`
}

type PerformSwitchoverOutput struct{}

func (a *Activities) ExecutePerformSwitchover(ctx workflow.Context, hostID string, input *PerformSwitchoverInput) workflow.Future[*PerformSwitchoverOutput] {
	opts := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*PerformSwitchoverOutput](ctx, opts, a.PerformSwitchover, input)
}

func (a *Activities) PerformSwitchover(ctx context.Context, input *PerformSwitchoverInput) (*PerformSwitchoverOutput, error) {
	logger := activity.Logger(ctx)
	if input == nil {
		return nil, errors.New("input is nil")
	}
	logger = logger.With("database_id", input.DatabaseID, "task_id", input.TaskID.String())

	orch, err := do.Invoke[database.Orchestrator](a.Injector)
	if err != nil {
		return nil, fmt.Errorf("failed to get orchestrator: %w", err)
	}

	connInfo, err := orch.GetInstanceConnectionInfo(ctx, input.DatabaseID, input.LeaderInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance connection info for leader: %w", err)
	}

	pClient := patroni.NewClient(connInfo.PatroniURL(), nil)

	leaderName := &input.LeaderInstanceID
	swReq := &patroni.Switchover{
		Leader: leaderName,
	}
	if input.CandidateInstanceID != "" {
		swReq.Candidate = &input.CandidateInstanceID
	}
	if !input.ScheduledAt.IsZero() {
		swReq.ScheduledAt = &input.ScheduledAt
	}

	if err := pClient.Switchover(ctx, swReq); err != nil {
		return nil, fmt.Errorf("patroni switchover call failed: %w", err)
	}

	logger.Info("patroni switchover request sent")
	return &PerformSwitchoverOutput{}, nil
}
