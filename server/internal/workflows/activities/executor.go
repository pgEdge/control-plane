package activities

import (
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

var (
	ErrExecutorNotFound = errors.New("executor not found")
	ErrHostRemoved      = errors.New("host with the given ID has been removed")
)

func (a *Activities) ResolveExecutor(state *resource.State, executor resource.Executor) (core.Queue, error) {
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
	case resource.ExecutorTypeHost:
		return utils.HostQueue(executor.ID), nil
	case resource.ExecutorTypeManager:
		return utils.ManagerQueue(), nil
	case resource.ExecutorTypeAny:
		return utils.AnyQueue(), nil
	case resource.ExecutorTypePrimary:
		node, err := resource.FromContext[*database.NodeResource](rc, database.NodeResourceIdentifier(executor.ID))
		if errors.Is(err, resource.ErrNotFound) {
			return "", ErrExecutorNotFound
		} else if err != nil {
			return "", fmt.Errorf("failed to get node resource: %w", err)
		}
		if node.PrimaryInstanceID == "" {
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
		if instance.Spec.HostID == "" {
			// This should be impossible
			return "", fmt.Errorf("instance %s has no host ID", instance.Spec.InstanceID)
		}
		return utils.HostQueue(instance.Spec.HostID), nil
	default:
		return "", fmt.Errorf("unknown executor type: %s", executor.Type)
	}
}
