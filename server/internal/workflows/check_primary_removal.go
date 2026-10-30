package workflows

import (
	"errors"
	"fmt"
	"slices"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

// checkPrimaryNotRemoved returns an error if the given spec would remove the
// current primary instance of a node that remains in the spec. This mirrors
// the API validation so that we fail fast if that validation is bypassed or
// if a switchover happened after the request was validated. Instances on hosts
// in removeHosts are exempt because those hosts are being forcibly removed.
func checkPrimaryNotRemoved(spec *database.Spec, current *resource.State, removeHosts []string) error {
	removed := ds.NewSet(removeHosts...)

	var errs []error
	for _, node := range spec.Nodes {
		nodeRes, err := resource.FromState[*database.NodeResource](current, database.NodeResourceIdentifier(node.Name))
		if errors.Is(err, resource.ErrNotFound) {
			// New node, nothing to remove.
			continue
		} else if err != nil {
			return fmt.Errorf("failed to get node %s from current state: %w", node.Name, err)
		}

		primaryID := nodeRes.PrimaryInstanceID
		if primaryID == "" {
			// We couldn't determine the primary, e.g. because Patroni was
			// unreachable from every instance during the refresh. Fail closed
			// so that we don't delete a primary we failed to recognize.
			for _, instanceID := range nodeRes.InstanceIDs {
				hostID, err := removedInstanceHostID(current, node, removed, instanceID)
				if err != nil {
					return err
				}
				if hostID == "" {
					continue
				}
				errs = append(errs, fmt.Errorf(
					"cannot remove host %s from node %s because the node's primary instance could not be determined; retry once the primary is reachable",
					hostID, node.Name,
				))
			}
			continue
		}

		hostID, err := removedInstanceHostID(current, node, removed, primaryID)
		if err != nil {
			return err
		}
		if hostID == "" {
			continue
		}
		errs = append(errs, fmt.Errorf(
			"cannot remove host %s from node %s because it runs the node's primary instance %s; perform a switchover to another instance before removing it",
			hostID, node.Name, primaryID,
		))
	}

	return errors.Join(errs...)
}

// removedInstanceHostID returns the host ID of the given instance if this
// update would delete it, or an empty string otherwise.
func removedInstanceHostID(
	current *resource.State,
	node *database.Node,
	removed ds.Set[string],
	instanceID string,
) (string, error) {
	data, ok := current.Get(database.InstanceResourceIdentifier(instanceID))
	if !ok {
		// The instance isn't in our state, so this update can't delete it.
		return "", nil
	}
	instance, err := resource.ToResource[*database.InstanceResource](data)
	if err != nil {
		return "", fmt.Errorf("failed to get instance %s from current state: %w", instanceID, err)
	}
	hostID := instance.Spec.HostID
	if slices.Contains(node.HostIDs, hostID) || removed.Has(hostID) {
		return "", nil
	}

	return hostID, nil
}
