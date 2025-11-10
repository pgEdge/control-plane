package workflows

import (
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type ValidateSpecInput struct {
	DatabaseID   string
	Spec         *database.Spec
	PreviousSpec *database.Spec
}

type ValidateSpecOutput struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

func (o *ValidateSpecOutput) merge(results []*database.ValidationResult) {
	for _, r := range results {
		if r.Valid {
			continue
		}
		o.Valid = false
		for _, err := range r.Errors {
			msg := fmt.Sprintf("validation error for node %s, host %s: %s", r.NodeName, r.HostID, err)
			o.Errors = append(o.Errors, msg)
		}
	}
}

func (w *Workflows) ValidateSpec(ctx workflow.Context, input *ValidateSpecInput) (*ValidateSpecOutput, error) {
	databaseID := input.DatabaseID

	logger := workflow.Logger(ctx).With("database_id", databaseID)
	logger.Info("starting database spec validation")

	instancesByHost, err := w.getInstancesByHost(ctx, input.Spec, input.PreviousSpec)
	if err != nil {
		logger.Error("failed to get instances by host", "error", err)
		return nil, fmt.Errorf("failed to get instances by host: %w", err)
	}

	var futures []workflow.Future[*activities.ValidateInstanceSpecsOutput]
	for hostID, grp := range instancesByHost {
		if len(grp.Current) == 0 {
			continue
		}
		input := &activities.ValidateInstanceSpecsInput{
			DatabaseID:    databaseID,
			Specs:         grp.Current,
			PreviousSpecs: grp.Previous,
		}

		future := w.Activities.ExecuteValidateInstanceSpecs(ctx, hostID, input)
		futures = append(futures, future)
	}

	overallResult := &ValidateSpecOutput{Valid: true}

	var allErrors []error
	for _, instanceFuture := range futures {
		output, err := instanceFuture.Get(ctx)
		if err != nil {
			allErrors = append(allErrors, err)
			continue
		}
		overallResult.merge(output.Results)
	}

	if err := errors.Join(allErrors...); err != nil {
		logger.Error("failed to validate instances", "error", err)
		return nil, fmt.Errorf("failed to validate instances: %w", err)
	}

	logger.Info("instance validation succeeded")
	return overallResult, nil
}

type hostGroup struct {
	Current  []*database.InstanceSpec
	Previous []*database.InstanceSpec
}

func (w *Workflows) getInstancesByHost(
	ctx workflow.Context,
	spec *database.Spec,
	prev *database.Spec,
) (map[string]hostGroup, error) {

	curNodes, err := spec.NodeInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to get node instances (current): %w", err)
	}
	curByHost := indexInstancesByHost(curNodes)

	var prevByHost map[string][]*database.InstanceSpec
	if prev != nil {
		prevNodes, perr := prev.NodeInstances()
		if perr != nil {
			return nil, fmt.Errorf("failed to get node instances (previous): %w", perr)
		}
		prevByHost = indexInstancesByHost(prevNodes)
	} else {
		prevByHost = make(map[string][]*database.InstanceSpec, 0)
	}

	res := make(map[string]hostGroup, len(curByHost))
	for hostID, curr := range curByHost {
		res[hostID] = hostGroup{
			Current:  curr,
			Previous: prevByHost[hostID],
		}
	}

	return res, nil
}

func indexInstancesByHost(nodes []*database.NodeInstances) map[string][]*database.InstanceSpec {
	mp := make(map[string][]*database.InstanceSpec, len(nodes))
	for _, n := range nodes {
		for _, inst := range n.Instances {
			if inst == nil || inst.HostID == "" {
				continue
			}
			mp[inst.HostID] = append(mp[inst.HostID], inst)
		}
	}
	return mp
}
