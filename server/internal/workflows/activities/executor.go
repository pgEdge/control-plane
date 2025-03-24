package activities

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
)

var ErrExecutorNotFound = fmt.Errorf("executor not found")

func (a *Activities) ResolveExecutor(state *resource.State, executor resource.Executor) (string, error) {
	registry, err := do.Invoke[*resource.Registry](a.Injector)
	if err != nil {
		return "", err
	}
	rc := &resource.Context{
		State:    state,
		Injector: a.Injector,
		Registry: registry,
	}

	switch executor.Type {
	case resource.ExecutorTypeHost, resource.ExecutorTypeCluster, resource.ExecutorTypeCohort:
		return executor.ID, nil
	case resource.ExecutorTypeNode:
		node, err := resource.FromContext[*database.NodeResource](rc, database.NodeResourceIdentifier(executor.ID))
		if errors.Is(err, resource.ErrNotFound) {
			return "", ErrExecutorNotFound
		} else if err != nil {
			return "", fmt.Errorf("failed to get node resource: %w", err)
		}
		if node.PrimaryInstanceID == uuid.Nil {
			// If this happens then whichever resource is using this executor
			// is probably missing the node in its dependencies.
			return "", fmt.Errorf("node %s has no primary instance", node.Name)
		}
		instance, err := resource.FromContext[*database.InstanceResource](rc, database.InstanceResourceIdentifier(node.PrimaryInstanceID))
		if errors.Is(err, resource.ErrNotFound) {
			return "", ErrExecutorNotFound
		} else if err != nil {
			return "", fmt.Errorf("failed to get instance resource: %w", err)
		}
		if instance.Spec.HostID == uuid.Nil {
			// This should be impossible
			return "", fmt.Errorf("instance %s has no host ID", instance.Spec.InstanceID)
		}
		return instance.Spec.HostID.String(), nil
	default:
		return "", fmt.Errorf("unknown executor type: %s", executor.Type)
	}
}
