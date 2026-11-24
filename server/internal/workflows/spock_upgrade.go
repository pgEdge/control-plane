package workflows

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

// spockInstances are the existing instances of a database that an update
// keeps, for the steps that run on each node's primary during a Spock 6
// update.
type spockInstances struct {
	DatabaseID   string
	DatabaseName string
	// Instances are sorted by node name, then instance ID, so that every
	// workflow replay schedules the same activities in the same order.
	Instances []spockUpgradeInstance
}

// spockUpgrade describes a Spock major version upgrade of an existing
// database. Validation guarantees that such an update doesn't add or move
// nodes, so the kept instances are the same before and after the update.
type spockUpgrade struct {
	spockInstances
	// RaisesMajor is true when some instance still runs an older Spock major
	// version. When false, every instance already reached the new version
	// and the update finishes an earlier upgrade's remaining steps.
	RaisesMajor bool
}

type spockUpgradeInstance struct {
	NodeName   string
	InstanceID string
	HostID     string
}

// keptSpockInstances returns the existing instances that the update keeps:
// the ones whose node and host are still in spec, leaving out hosts that are
// being removed. It also reports whether any of them runs a Spock major
// version older than spec's.
func keptSpockInstances(current *resource.State, spec *database.Spec, removeHosts []string) (*spockInstances, bool, error) {
	var targetMajor uint64
	if spec.SpockVersion != "" {
		target, err := ds.ParseVersion(spec.SpockVersion)
		if err != nil {
			return nil, false, fmt.Errorf("failed to parse spock version from spec: %w", err)
		}
		targetMajor, _ = target.Major()
	}

	instances, err := resource.AllFromState[*database.InstanceResource](current, database.ResourceTypeInstance)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get instances from current state: %w", err)
	}

	kept := &spockInstances{
		DatabaseID:   spec.DatabaseID,
		DatabaseName: spec.DatabaseName,
	}
	raisesMajor := false
	for _, instance := range instances {
		if instance.Spec == nil || slices.Contains(removeHosts, instance.Spec.HostID) {
			continue
		}
		node, err := spec.Node(instance.Spec.NodeName)
		if err != nil || !slices.Contains(node.HostIDs, instance.Spec.HostID) {
			continue
		}
		kept.Instances = append(kept.Instances, spockUpgradeInstance{
			NodeName:   instance.Spec.NodeName,
			InstanceID: instance.Spec.InstanceID,
			HostID:     instance.Spec.HostID,
		})
		version := instance.Spec.PgEdgeVersion
		if version == nil || !version.HasSpock() {
			continue
		}
		if major, ok := version.SpockVersion.Major(); ok && major < targetMajor {
			raisesMajor = true
		}
	}
	slices.SortFunc(kept.Instances, func(a, b spockUpgradeInstance) int {
		return cmp.Or(cmp.Compare(a.NodeName, b.NodeName), cmp.Compare(a.InstanceID, b.InstanceID))
	})

	return kept, raisesMajor, nil
}

// detectSpockUpgrade returns the upgrade when spec raises the Spock major
// version of any kept instance, or when pending reports that an earlier
// upgrade hasn't finished. It returns nil otherwise.
func detectSpockUpgrade(current *resource.State, spec *database.Spec, removeHosts []string, pending bool) (*spockUpgrade, error) {
	if spec.SpockVersion == "" {
		return nil, nil
	}
	kept, raisesMajor, err := keptSpockInstances(current, spec, removeHosts)
	if err != nil {
		return nil, err
	}
	if len(kept.Instances) == 0 || (!raisesMajor && !pending) {
		return nil, nil
	}
	return &spockUpgrade{spockInstances: *kept, RaisesMajor: raisesMajor}, nil
}

// fixesSpockBeta1Catalog reports whether the update checks each node for a
// Spock 6.0.0-beta1 catalog after the node is updated, and fixes it. See
// activities.FixSpockBeta1Catalog. Remove at Spock 6.0.0 GA.
func fixesSpockBeta1Catalog(spec *database.Spec) bool {
	if spec.SpockVersion == "" {
		return false
	}
	version, err := ds.ParseVersion(spec.SpockVersion)
	if err != nil {
		return false
	}
	major, ok := version.Major()
	return ok && major == 6
}

// nodesUpdatedByPlan groups the nodes by the plan that finishes updating
// them: the last plan with an event for one of the node's instances. The
// result has one more group than plans, for the nodes that no plan changes.
// Each node is given by its first instance.
func (s *spockInstances) nodesUpdatedByPlan(plans []resource.Plan) [][]spockUpgradeInstance {
	groups := make([][]spockUpgradeInstance, len(plans)+1)
	for _, node := range s.nodes() {
		last := len(plans)
		for i, plan := range plans {
			if s.planChangesNode(plan, node.NodeName) {
				last = i
			}
		}
		groups[last] = append(groups[last], node)
	}
	return groups
}

func (s *spockInstances) planChangesNode(plan resource.Plan, nodeName string) bool {
	for _, phase := range plan {
		for _, event := range phase {
			if event.Resource == nil || event.Resource.Identifier.Type != database.ResourceTypeInstance {
				continue
			}
			instance, ok := s.instance(event.Resource.Identifier.ID)
			if ok && instance.NodeName == nodeName {
				return true
			}
		}
	}
	return false
}

// nodes returns the node names in order, each with one of its instances to
// ask for the node's current primary.
func (s *spockInstances) nodes() []spockUpgradeInstance {
	var nodes []spockUpgradeInstance
	for _, instance := range s.Instances {
		if len(nodes) == 0 || nodes[len(nodes)-1].NodeName != instance.NodeName {
			nodes = append(nodes, instance)
		}
	}
	return nodes
}

func (s *spockInstances) instance(instanceID string) (spockUpgradeInstance, bool) {
	for _, instance := range s.Instances {
		if instance.InstanceID == instanceID {
			return instance, true
		}
	}
	return spockUpgradeInstance{}, false
}

// setSpockDDLReplication pauses or resumes Spock's automatic DDL replication
// on every instance, so that no schema change runs while nodes run different
// Spock major versions. When resuming, it tries every instance and returns
// all failures.
func (w *Workflows) setSpockDDLReplication(ctx workflow.Context, u *spockUpgrade, paused bool) error {
	futures := make([]workflow.Future[*activities.SetSpockDDLReplicationOutput], len(u.Instances))
	for i, instance := range u.Instances {
		futures[i] = w.Activities.ExecuteSetSpockDDLReplication(ctx, instance.HostID, &activities.SetSpockDDLReplicationInput{
			DatabaseID: u.DatabaseID,
			InstanceID: instance.InstanceID,
			Paused:     paused,
		})
	}
	var errs []error
	for _, future := range futures {
		if _, err := future.Get(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		if paused {
			return fmt.Errorf("failed to pause spock ddl replication: %w", err)
		}
		return fmt.Errorf("failed to resume spock ddl replication: %w", err)
	}
	return nil
}

// primaryInstances returns each node's current primary instance.
func (w *Workflows) primaryInstances(ctx workflow.Context, s *spockInstances) ([]spockUpgradeInstance, error) {
	var primaries []spockUpgradeInstance
	for _, node := range s.nodes() {
		primary, err := w.nodePrimary(ctx, s, node)
		if err != nil {
			return nil, err
		}
		primaries = append(primaries, primary)
	}
	return primaries, nil
}

// nodePrimary returns the current primary of the given node, asking the
// given instance of it.
func (w *Workflows) nodePrimary(ctx workflow.Context, s *spockInstances, node spockUpgradeInstance) (spockUpgradeInstance, error) {
	out, err := w.Activities.ExecuteGetPrimaryInstance(ctx, node.HostID, &activities.GetPrimaryInstanceInput{
		DatabaseID: s.DatabaseID,
		InstanceID: node.InstanceID,
	}).Get(ctx)
	if err != nil {
		return spockUpgradeInstance{}, fmt.Errorf("failed to get the primary instance of node %s: %w", node.NodeName, err)
	}
	primary, ok := s.instance(out.PrimaryInstanceID)
	if !ok || primary.NodeName != node.NodeName {
		return spockUpgradeInstance{}, fmt.Errorf("node %s reports primary instance %q, which isn't one of its instances", node.NodeName, out.PrimaryInstanceID)
	}
	return primary, nil
}

// prepareSpockUpgrade runs before the rolling update: it flags every node
// primary's Spock slots for failover, so that native slot sync on upgraded
// Postgres 17+ replicas copies them before those replicas can be promoted.
func (w *Workflows) prepareSpockUpgrade(ctx workflow.Context, u *spockUpgrade) error {
	primaries, err := w.primaryInstances(ctx, &u.spockInstances)
	if err != nil {
		return err
	}
	for _, primary := range primaries {
		_, err := w.Activities.ExecuteEnableSpockSlotsFailover(ctx, primary.HostID, &activities.EnableSpockSlotsFailoverInput{
			DatabaseID:   u.DatabaseID,
			InstanceID:   primary.InstanceID,
			DatabaseName: u.DatabaseName,
		}).Get(ctx)
		if err != nil {
			return fmt.Errorf("failed to enable failover on the replication slots of node %s: %w", primary.NodeName, err)
		}
	}
	return nil
}

// verifySpockUpgrade checks every node's current primary once the rolling
// update finishes. Each node was already checked right after its own update,
// but a node checked early may depend on nodes upgraded after it. See
// activities.VerifySpockUpgrade.
func (w *Workflows) verifySpockUpgrade(ctx workflow.Context, u *spockUpgrade) error {
	primaries, err := w.primaryInstances(ctx, &u.spockInstances)
	if err != nil {
		return err
	}
	for _, primary := range primaries {
		if err := w.verifySpockUpgradeOn(ctx, u, primary); err != nil {
			return err
		}
	}
	return nil
}

func (w *Workflows) verifySpockUpgradeOn(ctx workflow.Context, u *spockUpgrade, primary spockUpgradeInstance) error {
	_, err := w.Activities.ExecuteVerifySpockUpgrade(ctx, primary.HostID, &activities.VerifySpockUpgradeInput{
		DatabaseID:   u.DatabaseID,
		InstanceID:   primary.InstanceID,
		DatabaseName: u.DatabaseName,
	}).Get(ctx)
	if err != nil {
		return fmt.Errorf("spock upgrade verification failed on node %s: %w", primary.NodeName, err)
	}
	return nil
}

// spockNodeSteps runs the Spock steps for each node once the rolling update
// has finished updating it, before the next node is updated.
type spockNodeSteps struct {
	databaseID string
	taskID     uuid.UUID
	nodes      *spockInstances
	// upgrade is set during a Spock major upgrade, to verify each node.
	upgrade *spockUpgrade
	// fixBeta1Catalog is set to fix a Spock 6.0.0-beta1 catalog on each node.
	fixBeta1Catalog bool
	// groups are the nodes that each plan finishes, plus a last group for the
	// nodes that no plan changes. See spockInstances.nodesUpdatedByPlan.
	groups [][]spockUpgradeInstance
}

// newSpockNodeSteps returns nil when there's nothing to run for each node.
func newSpockNodeSteps(
	databaseID string,
	taskID uuid.UUID,
	nodes *spockInstances,
	upgrade *spockUpgrade,
	fixBeta1Catalog bool,
	plans []resource.Plan,
) *spockNodeSteps {
	if nodes == nil || len(nodes.Instances) == 0 || (upgrade == nil && !fixBeta1Catalog) {
		return nil
	}
	return &spockNodeSteps{
		databaseID:      databaseID,
		taskID:          taskID,
		nodes:           nodes,
		upgrade:         upgrade,
		fixBeta1Catalog: fixBeta1Catalog,
		groups:          nodes.nodesUpdatedByPlan(plans),
	}
}

// run runs the steps for the nodes in the given group.
func (n *spockNodeSteps) run(ctx workflow.Context, w *Workflows, group int) error {
	if n == nil {
		return nil
	}
	for _, node := range n.groups[group] {
		primary, err := w.nodePrimary(ctx, n.nodes, node)
		if err != nil {
			return err
		}
		if n.fixBeta1Catalog {
			out, err := w.Activities.ExecuteFixSpockBeta1Catalog(ctx, primary.HostID, &activities.FixSpockBeta1CatalogInput{
				DatabaseID:   n.databaseID,
				InstanceID:   primary.InstanceID,
				DatabaseName: n.nodes.DatabaseName,
			}).Get(ctx)
			if err != nil {
				return fmt.Errorf("failed to check the spock catalog on node %s: %w", node.NodeName, err)
			}
			if out.Fixed {
				w.logSpockUpgradeEvent(ctx, n.databaseID, n.taskID, fmt.Sprintf("updated the spock 6.0.0-beta1 catalog on node %s", node.NodeName))
			}
		}
		if n.upgrade != nil {
			w.logSpockUpgradeEvent(ctx, n.databaseID, n.taskID, fmt.Sprintf("verifying the spock upgrade on node %s", node.NodeName))
			if err := w.verifySpockUpgradeOn(ctx, n.upgrade, primary); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *Workflows) logSpockUpgradeEvent(ctx workflow.Context, databaseID string, taskID uuid.UUID, message string) {
	err := w.logTaskEvent(ctx, task.ScopeDatabase, databaseID, taskID, task.LogEntry{Message: message})
	if err != nil {
		workflow.Logger(ctx).With("error", err).Warn("failed to log task event")
	}
}
