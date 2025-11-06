package workflows

import (
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type ValidateSpecInput struct {
	DatabaseID          string
	Spec                *database.Spec
	PreviousSpec        *database.Spec
	ValidateOnlyUpdated bool
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

	instancesByHost, err := w.getInstancesByHost(ctx, input.Spec, input.PreviousSpec, input.ValidateOnlyUpdated)
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
	prev *database.Spec,
	validateOnlyUpdated bool,
) ([][]*database.InstanceSpec, error) {
	nodeInstances, err := spec.NodeInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to get node instances: %w", err)
	}

	var prevIndex map[string]*database.InstanceSpec
	if prev != nil {
		prevIndex = indexInstances(prev)
	}

	fut := workflow.SideEffect(ctx, func(_ workflow.Context) [][]*database.InstanceSpec {
		byHost := make(map[string][]*database.InstanceSpec)
		for _, node := range nodeInstances {
			for _, inst := range node.Instances {
				nodeName := inst.NodeName
				if validateOnlyUpdated && prevIndex != nil {
					if !instanceChanged(nodeName, inst, prevIndex) {
						continue
					}
					if onlyPortChanged(nodeName, inst, prevIndex) && !portNeedsValidation(nodeName, inst, prevIndex) {
						continue
					}
				}
				byHost[inst.HostID] = append(byHost[inst.HostID], inst)
			}
		}

		res := make([][]*database.InstanceSpec, 0, len(byHost))
		for _, group := range byHost {
			res = append(res, group)
		}
		return res
	})

	instances, err := fut.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to group instances by host: %w", err)
	}
	return instances, nil
}

func indexInstances(spec *database.Spec) map[string]*database.InstanceSpec {
	idx := make(map[string]*database.InstanceSpec)
	nodes, err := spec.NodeInstances()
	if err != nil {
		return idx
	}
	for _, n := range nodes {
		for _, inst := range n.Instances {
			nodeName := inst.NodeName
			idx[instKey(nodeName, inst.HostID)] = inst
		}
	}
	return idx
}

func instKey(nodeName, hostID string) string {
	return nodeName + "/" + hostID
}

func instanceChanged(nodeName string, cur *database.InstanceSpec, prevIndex map[string]*database.InstanceSpec) bool {
	prev := prevIndex[instKey(nodeName, cur.HostID)]
	if prev == nil {
		return true
	}

	if !equalPorts(cur.Port, prev.Port) {
		return true
	}

	if !equalOrchestratorOpts(cur, prev) {
		return true
	}
	return false
}

func onlyPortChanged(nodeName string, cur *database.InstanceSpec, prevIndex map[string]*database.InstanceSpec) bool {
	prev := prevIndex[instKey(nodeName, cur.HostID)]
	if prev == nil {
		return false
	}
	return !equalPorts(cur.Port, prev.Port)
}

func portNeedsValidation(nodeName string, cur *database.InstanceSpec, prevIndex map[string]*database.InstanceSpec) bool {
	prev := prevIndex[instKey(nodeName, cur.HostID)]
	curPort := derefPort(cur.Port)
	prevPort := 0
	if prev != nil {
		prevPort = derefPort(prev.Port)
	}

	if curPort == 0 {
		return false
	}

	return curPort != prevPort
}

func equalPorts(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func derefPort(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
func equalOrchestratorOpts(a, b *database.InstanceSpec) bool {
	ao, bo := a.OrchestratorOpts, b.OrchestratorOpts
	if (ao == nil) != (bo == nil) {
		return false
	}
	if ao == nil {
		return true
	}
	return reflect.DeepEqual(normalizeSwarm(ao.Swarm), normalizeSwarm(bo.Swarm))
}

type swarmNormalized struct {
	LabelsKV   [][2]string
	VolumeKeys []string
	Networks   []networkNorm
}

type networkNorm struct {
	ID       string
	Aliases  []string
	DriverKV [][2]string
}

func normalizeSwarm(s *database.SwarmOpts) *swarmNormalized {
	if s == nil {
		return nil
	}
	n := &swarmNormalized{}

	for k, v := range coalesceMap(s.ExtraLabels) {
		n.LabelsKV = append(n.LabelsKV, [2]string{k, v})
	}
	sort.Slice(n.LabelsKV, func(i, j int) bool { return n.LabelsKV[i][0] < n.LabelsKV[j][0] })

	for _, v := range s.ExtraVolumes {
		n.VolumeKeys = append(n.VolumeKeys, v.HostPath+"|"+v.DestinationPath)
	}
	sort.Strings(n.VolumeKeys)

	for _, nw := range s.ExtraNetworks {
		nn := networkNorm{ID: nw.ID}
		nn.Aliases = append(nn.Aliases, nw.Aliases...)
		sort.Strings(nn.Aliases)
		for k, v := range coalesceMap(nw.DriverOpts) {
			nn.DriverKV = append(nn.DriverKV, [2]string{k, v})
		}
		sort.Slice(nn.DriverKV, func(i, j int) bool { return nn.DriverKV[i][0] < nn.DriverKV[j][0] })

		n.Networks = append(n.Networks, nn)
	}
	sort.Slice(n.Networks, func(i, j int) bool { return n.Networks[i].ID < n.Networks[j].ID })

	return n
}

func coalesceMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
