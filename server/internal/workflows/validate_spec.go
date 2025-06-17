package workflows

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type ValidateSpecInput struct {
	DatabaseID string
	Spec       *database.Spec
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

	instancesByHost, err := w.getInstancesByHost(ctx, input.Spec)
	if err != nil {
		logger.Error("failed to get instances by host", "error", err)
		return nil, fmt.Errorf("failed to get instances by host: %w", err)
	}

	var futures []workflow.Future[*activities.ValidateInstanceSpecsOutput]
	for _, instances := range instancesByHost {
		if len(instances) < 1 {
			// Shouldn't happen, but want to be safe about the HostID access
			// below
			continue
		}
		input := &activities.ValidateInstanceSpecsInput{
			DatabaseID: databaseID,
			Specs:      instances,
		}

		future := w.Activities.ExecuteValidateInstanceSpecs(ctx, instances[0].HostID, input)
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

func (w *Workflows) getInstancesByHost(
	ctx workflow.Context,
	spec *database.Spec,
) ([][]*database.InstanceSpec, error) {
	nodeInstances, err := spec.NodeInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to get node instances: %w", err)
	}

	// Using a side effect here because this operation is non-deterministic.
	future := workflow.SideEffect(ctx, func(_ workflow.Context) [][]*database.InstanceSpec {
		byHost := map[string][]*database.InstanceSpec{}
		for _, node := range nodeInstances {
			for _, instance := range node.Instances {
				byHost[instance.HostID] = append(byHost[instance.HostID], instance)
			}
		}
		return slices.Collect(maps.Values(byHost))
	})

	instances, err := future.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to group instances by host: %w", err)
	}

	return instances, nil
}
