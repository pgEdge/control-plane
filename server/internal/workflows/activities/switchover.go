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
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

type InstanceHost struct {
	InstanceID string `json:"instance_id"`
	HostID     string `json:"host_id"`
}

// PerformSwitchoverInput.ScheduledAt is only set by switchover workflows
// started by older versions, which scheduled the switchover in Patroni. The
// Switchover workflow now waits for the scheduled time itself, and sets
// Deadline to the latest time the switchover may be requested from Patroni.
// Deadline is set from the workflow's clock and checked against this host's
// clock, so it assumes that the hosts' clocks are in sync, e.g. with NTP.
type PerformSwitchoverInput struct {
	DatabaseID          string    `json:"database_id"`
	LeaderInstanceID    string    `json:"leader_instance_id"`
	CandidateInstanceID string    `json:"candidate_instance_id,omitempty"`
	ScheduledAt         time.Time `json:"scheduled_at,omitempty"`
	Deadline            time.Time `json:"deadline,omitempty"`
	TaskID              uuid.UUID `json:"task_id"`
}

type PerformSwitchoverOutput struct{}

type GetNodeInstancesInput struct {
	DatabaseID string `json:"database_id"`
	NodeName   string `json:"node_name"`
}

type GetNodeInstancesOutput struct {
	Instances []*InstanceHost `json:"instances"`
	// UnreachableHosts are the hosts of Instances whose Control Plane isn't
	// reachable, so activities sent to them wouldn't run.
	UnreachableHosts []string `json:"unreachable_hosts,omitempty"`
}

func (a *Activities) ExecuteGetNodeInstances(ctx workflow.Context, input *GetNodeInstancesInput) workflow.Future[*GetNodeInstancesOutput] {
	opts := workflow.ActivityOptions{
		Queue: utils.AnyQueue(),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*GetNodeInstancesOutput](ctx, opts, a.GetNodeInstances, input)
}

// GetNodeInstances returns a node's current instances from the stored spec,
// and which of their hosts are unreachable. It fails if the database no longer
// exists or can't be modified.
func (a *Activities) GetNodeInstances(ctx context.Context, input *GetNodeInstancesInput) (*GetNodeInstancesOutput, error) {
	db, err := a.DatabaseService.GetDatabase(ctx, input.DatabaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to get database: %w", err)
	}
	if !database.DatabaseStateModifiable(db.State) {
		return nil, fmt.Errorf("database can't be modified in its current state: %s", db.State)
	}
	node, err := db.Spec.Node(input.NodeName)
	if err != nil {
		return nil, err
	}

	hostSvc, err := do.Invoke[*host.Service](a.Injector)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize host service: %w", err)
	}
	hosts, err := hostSvc.GetHosts(ctx, node.HostIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get hosts: %w", err)
	}
	reachable := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		if h.Status != nil && h.Status.State != host.HostStateUnreachable && h.Status.State != host.HostStateUnknown {
			reachable[h.ID] = true
		}
	}

	out := &GetNodeInstancesOutput{}
	for _, hostID := range node.HostIDs {
		out.Instances = append(out.Instances, &InstanceHost{
			InstanceID: database.InstanceIDFor(hostID, input.DatabaseID, node.Name),
			HostID:     hostID,
		})
		if !reachable[hostID] {
			out.UnreachableHosts = append(out.UnreachableHosts, hostID)
		}
	}
	return out, nil
}

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
	if input == nil {
		return nil, errors.New("input is nil")
	}
	if !input.Deadline.IsZero() && time.Now().After(input.Deadline) {
		return nil, fmt.Errorf("switchover not performed: it had to start by %s", input.Deadline.Format(time.RFC3339))
	}
	logger := activity.Logger(ctx).With("database_id", input.DatabaseID, "task_id", input.TaskID.String())

	connInfo, err := a.DatabaseService.GetInstanceConnectionInfo(ctx, input.DatabaseID, input.LeaderInstanceID)
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

	if err := pClient.ScheduleSwitchover(ctx, swReq, false); err != nil {
		return nil, fmt.Errorf("patroni scheduled switchover call failed: %w", err)
	}

	logger.Info("patroni scheduled switchover request sent")
	return &PerformSwitchoverOutput{}, nil
}

type CancelSwitchoverInput struct {
	DatabaseID       string    `json:"database_id"`
	LeaderInstanceID string    `json:"leader_instance_id"`
	TaskID           uuid.UUID `json:"task_id"`
}

type CancelSwitchoverOutput struct{}

func (a *Activities) ExecuteCancelSwitchover(ctx workflow.Context, hostID string, input *CancelSwitchoverInput) workflow.Future[*CancelSwitchoverOutput] {
	opts := workflow.ActivityOptions{
		Queue: utils.HostQueue(hostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*CancelSwitchoverOutput](ctx, opts, a.CancelSwitchover, input)
}

func (a *Activities) CancelSwitchover(ctx context.Context, input *CancelSwitchoverInput) (*CancelSwitchoverOutput, error) {
	logger := activity.Logger(ctx)
	logger = logger.With("database_id", input.DatabaseID, "task_id", input.TaskID.String())

	connInfo, err := a.DatabaseService.GetInstanceConnectionInfo(ctx, input.DatabaseID, input.LeaderInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance connection info for leader: %w", err)
	}

	pClient := patroni.NewClient(connInfo.PatroniURL(), nil)

	if err := pClient.CancelSwitchover(ctx); err != nil {
		return nil, fmt.Errorf("patroni cancel switchover call failed: %w", err)
	}

	logger.Info("patroni cancel switchover request sent")
	return &CancelSwitchoverOutput{}, nil
}
