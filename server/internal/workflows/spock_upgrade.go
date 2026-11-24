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

// spockUpgrade describes a Spock major version upgrade of an existing
// database. Validation guarantees that such an update doesn't add, remove, or
// move nodes, so the instances are the same before and after the update.
type spockUpgrade struct {
	DatabaseID   string
	DatabaseName string
	// Instances are sorted by node name, then instance ID, so that every
	// workflow replay schedules the same activities in the same order.
	Instances []spockUpgradeInstance
	// TargetMajor is the spec's Spock major version.
	TargetMajor uint64
	// RaisesMajor is true when some instance still runs an older Spock major
	// version, so the update upgrades Spock. When false, the instances are
	// only used to look for an earlier upgrade that didn't finish.
	RaisesMajor bool
}

// minSpockMajorWithUpgradeMarker is the first Spock major version that an
// upgrade can reach. Only databases on it or later can have an unfinished
// upgrade's DDL pause left behind.
const minSpockMajorWithUpgradeMarker = 6

type spockUpgradeInstance struct {
	NodeName   string
	InstanceID string
	HostID     string
}

// detectSpockUpgrade returns the database's Spock instances for the update,
// with RaisesMajor set when spec raises the Spock major version of any
// existing instance. It returns nil when the spec has no Spock version or
// there are no instances yet. Instances on hosts that are being removed are
// left out, since nothing can run on those hosts.
func detectSpockUpgrade(current *resource.State, spec *database.Spec, removeHosts []string) (*spockUpgrade, error) {
	if spec.SpockVersion == "" {
		return nil, nil
	}
	target, err := ds.ParseVersion(spec.SpockVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to parse spock version from spec: %w", err)
	}
	targetMajor, ok := target.Major()
	if !ok {
		return nil, nil
	}

	instances, err := resource.AllFromState[*database.InstanceResource](current, database.ResourceTypeInstance)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances from current state: %w", err)
	}

	upgrade := &spockUpgrade{
		DatabaseID:   spec.DatabaseID,
		DatabaseName: spec.DatabaseName,
		TargetMajor:  targetMajor,
	}
	for _, instance := range instances {
		if instance.Spec == nil || slices.Contains(removeHosts, instance.Spec.HostID) {
			continue
		}
		upgrade.Instances = append(upgrade.Instances, spockUpgradeInstance{
			NodeName:   instance.Spec.NodeName,
			InstanceID: instance.Spec.InstanceID,
			HostID:     instance.Spec.HostID,
		})
		version := instance.Spec.PgEdgeVersion
		if version == nil || !version.HasSpock() {
			continue
		}
		if major, ok := version.SpockVersion.Major(); ok && major < targetMajor {
			upgrade.RaisesMajor = true
		}
	}
	if len(upgrade.Instances) == 0 {
		return nil, nil
	}
	slices.SortFunc(upgrade.Instances, func(a, b spockUpgradeInstance) int {
		return cmp.Or(cmp.Compare(a.NodeName, b.NodeName), cmp.Compare(a.InstanceID, b.InstanceID))
	})

	return upgrade, nil
}

// mayHaveUnfinishedUpgrade reports whether the database could have DDL
// replication paused by an earlier Spock major upgrade that didn't finish.
func (u *spockUpgrade) mayHaveUnfinishedUpgrade() bool {
	return !u.RaisesMajor && u.TargetMajor >= minSpockMajorWithUpgradeMarker
}

// spockDDLReplicationPaused reports whether any instance still has DDL
// replication paused by an earlier Spock major upgrade, meaning that upgrade
// didn't finish its verification or resume. An instance that can't be
// checked is logged and skipped, so a down instance doesn't block the update.
func (w *Workflows) spockDDLReplicationPaused(ctx workflow.Context, u *spockUpgrade) bool {
	futures := make([]workflow.Future[*activities.GetSpockDDLReplicationPausedOutput], len(u.Instances))
	for i, instance := range u.Instances {
		futures[i] = w.Activities.ExecuteGetSpockDDLReplicationPaused(ctx, instance.HostID, &activities.GetSpockDDLReplicationPausedInput{
			DatabaseID: u.DatabaseID,
			InstanceID: instance.InstanceID,
		})
	}
	paused := false
	for i, future := range futures {
		out, err := future.Get(ctx)
		if err != nil {
			workflow.Logger(ctx).With("error", err, "instance_id", u.Instances[i].InstanceID).
				Warn("failed to check for a paused spock ddl replication")
			continue
		}
		paused = paused || out.Paused
	}
	return paused
}

// nodes returns the upgrade's node names in order, each with one of its
// instances to ask for the node's current primary.
func (u *spockUpgrade) nodes() []spockUpgradeInstance {
	var nodes []spockUpgradeInstance
	for _, instance := range u.Instances {
		if len(nodes) == 0 || nodes[len(nodes)-1].NodeName != instance.NodeName {
			nodes = append(nodes, instance)
		}
	}
	return nodes
}

func (u *spockUpgrade) instance(instanceID string) (spockUpgradeInstance, bool) {
	for _, instance := range u.Instances {
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
func (w *Workflows) primaryInstances(ctx workflow.Context, u *spockUpgrade) ([]spockUpgradeInstance, error) {
	var primaries []spockUpgradeInstance
	for _, node := range u.nodes() {
		out, err := w.Activities.ExecuteGetPrimaryInstance(ctx, node.HostID, &activities.GetPrimaryInstanceInput{
			DatabaseID: u.DatabaseID,
			InstanceID: node.InstanceID,
		}).Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get the primary instance of node %s: %w", node.NodeName, err)
		}
		primary, ok := u.instance(out.PrimaryInstanceID)
		if !ok {
			return nil, fmt.Errorf("node %s reports primary instance %q, which isn't one of its instances", node.NodeName, out.PrimaryInstanceID)
		}
		primaries = append(primaries, primary)
	}
	return primaries, nil
}

// prepareSpockUpgrade runs before the rolling update: it flags every node
// primary's Spock slots for failover, so that native slot sync on upgraded
// Postgres 17+ replicas copies them before those replicas can be promoted.
func (w *Workflows) prepareSpockUpgrade(ctx workflow.Context, u *spockUpgrade) error {
	primaries, err := w.primaryInstances(ctx, u)
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

// verifySpockUpgrade runs after the rolling update and checks each node's
// current primary. See activities.VerifySpockUpgrade.
func (w *Workflows) verifySpockUpgrade(ctx workflow.Context, u *spockUpgrade) error {
	primaries, err := w.primaryInstances(ctx, u)
	if err != nil {
		return err
	}
	for _, primary := range primaries {
		_, err := w.Activities.ExecuteVerifySpockUpgrade(ctx, primary.HostID, &activities.VerifySpockUpgradeInput{
			DatabaseID:   u.DatabaseID,
			InstanceID:   primary.InstanceID,
			DatabaseName: u.DatabaseName,
		}).Get(ctx)
		if err != nil {
			return fmt.Errorf("spock upgrade verification failed on node %s: %w", primary.NodeName, err)
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
