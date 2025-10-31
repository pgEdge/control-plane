package activities

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/pgEdge/control-plane/server/internal/monitor"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/samber/do"
)

type SelectCandidateInput struct {
	DatabaseID      string          `json:"database_id"`
	NodeName        string          `json:"node_name"`
	ExcludeInstance string          `json:"exclude_instance"`
	Instances       []*InstanceHost `json:"instances,omitempty"` // optional
}

type SelectCandidateOutput struct {
	CandidateInstanceID string `json:"candidate_instance_id"`
	CandidateHostID     string `json:"candidate_host_id,omitempty"`
}

func (a *Activities) ExecuteSelectCandidate(ctx workflow.Context, input *SelectCandidateInput) workflow.Future[*SelectCandidateOutput] {
	opts := workflow.ActivityOptions{
		Queue: utils.AnyQueue(),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.ExecuteActivity[*SelectCandidateOutput](ctx, opts, a.SelectCandidate, input)
}

func (a *Activities) SelectCandidate(ctx context.Context, input *SelectCandidateInput) (*SelectCandidateOutput, error) {
	if input == nil {
		return nil, fmt.Errorf("ExecuteSelectCandidate: input is nil")
	}

	makeOutput := func(inst *InstanceHost) *SelectCandidateOutput {
		if inst == nil {
			return nil
		}
		return &SelectCandidateOutput{
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

	return nil, fmt.Errorf("ExecuteSelectCandidate: no eligible candidate found for database=%s node=%s", input.DatabaseID, input.NodeName)
}
