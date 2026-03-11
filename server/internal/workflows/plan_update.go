package workflows

import (
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/database/operations"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/monitor"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type PlanUpdateInput struct {
	Options operations.UpdateDatabaseOptions `json:"options"`
	Spec    *database.Spec                   `json:"spec"`
	Current *resource.State                  `json:"current"`
}

type PlanUpdateOutput struct {
	Plans []resource.Plan `json:"plans"`
}

func (w *Workflows) ExecutePlanUpdate(
	ctx workflow.Context,
	input *PlanUpdateInput,
) workflow.Future[*PlanUpdateOutput] {
	options := workflow.SubWorkflowOptions{
		Queue: utils.HostQueue(w.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.CreateSubWorkflowInstance[*PlanUpdateOutput](ctx, options, w.PlanUpdate, input)
}

func (w *Workflows) PlanUpdate(ctx workflow.Context, input *PlanUpdateInput) (*PlanUpdateOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID)
	logger.Info("getting desired state")

	nodeInstances, err := input.Spec.NodeInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to get node instances: %w", err)
	}

	nodeResources := make([]*operations.NodeResources, len(nodeInstances))
	for i, node := range nodeInstances {
		resources, err := w.getNodeResources(ctx, node)
		if err != nil {
			return nil, err
		}

		nodeResources[i] = resources
	}

	// Generate service instance resources.
	// Pick any node name for ServiceUserRole PrimaryExecutor routing.
	var nodeName string
	if len(nodeInstances) > 0 {
		nodeName = nodeInstances[0].NodeName
	}
	if nodeName == "" && len(input.Spec.Services) > 0 {
		return nil, fmt.Errorf("no database nodes available for service role routing")
	}

	var serviceResources []*operations.ServiceResources
	for _, serviceSpec := range input.Spec.Services {
		for _, hostID := range serviceSpec.HostIDs {
			svcRes, err := w.getServiceResources(ctx, input.Spec, serviceSpec, hostID, nodeName, nodeInstances)
			if err != nil {
				return nil, fmt.Errorf("failed to get service resources for %s on %s: %w", serviceSpec.ServiceID, hostID, err)
			}
			serviceResources = append(serviceResources, svcRes)
		}
	}

	plans, err := operations.UpdateDatabase(input.Options, input.Current, nodeResources, serviceResources)
	if err != nil {
		return nil, fmt.Errorf("failed to plan database update: %w", err)
	}

	logger.Info("successfully planned database update")

	return &PlanUpdateOutput{Plans: plans}, nil
}

func (w *Workflows) getServiceResources(
	ctx workflow.Context,
	spec *database.Spec,
	serviceSpec *database.ServiceSpec,
	hostID string,
	nodeName string,
	nodeInstances []*database.NodeInstances,
) (*operations.ServiceResources, error) {
	serviceInstanceID := database.GenerateServiceInstanceID(spec.DatabaseID, serviceSpec.ServiceID, hostID)
	pgEdgeVersion, err := host.NewPgEdgeVersion(spec.PostgresVersion, spec.SpockVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pgedge version: %w", err)
	}

	// Resolve Postgres connection info for the service container.
	// Services connect to Postgres via the overlay network using the instance hostname.
	databaseHost, databasePort, err := findPostgresInstance(nodeInstances, hostID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve postgres instance for service: %w", err)
	}

	serviceInstanceSpec := &database.ServiceInstanceSpec{
		ServiceInstanceID: serviceInstanceID,
		ServiceSpec:       serviceSpec,
		PgEdgeVersion:     pgEdgeVersion,
		DatabaseID:        spec.DatabaseID,
		DatabaseName:      spec.DatabaseName,
		HostID:            hostID,
		NodeName:          nodeName,
		DatabaseNetworkID: database.GenerateDatabaseNetworkID(spec.DatabaseID),
		DatabaseHost:      databaseHost,
		DatabasePort:      databasePort,
		Port:              serviceSpec.Port,
		// Credentials: nil — ServiceUserRole.Create() will generate them
	}

	generateInput := &activities.GenerateServiceInstanceResourcesInput{Spec: serviceInstanceSpec}
	generateOutput, err := w.Activities.ExecuteGenerateServiceInstanceResources(ctx, generateInput).Get(ctx)
	if err != nil {
		return nil, err
	}

	return &operations.ServiceResources{
		ServiceInstanceID: serviceInstanceID,
		Resources:         generateOutput.Resources.Resources,
		MonitorResource: &monitor.ServiceInstanceMonitorResource{
			DatabaseID:        spec.DatabaseID,
			ServiceInstanceID: serviceInstanceID,
			HostID:            hostID,
		},
	}, nil
}

// findPostgresInstance resolves the Postgres hostname and port for a service
// container from the database spec. It prefers a co-located instance (same host
// as the service) for lower latency, falling back to any instance in the database.
// The hostname follows the swarm orchestrator convention: "postgres-{instanceID}".
// The returned port is always the internal container port (5432), not the published
// host port, because service containers connect via the overlay network.
func findPostgresInstance(nodeInstances []*database.NodeInstances, serviceHostID string) (string, int, error) {
	const internalPort = 5432

	var fallback *database.InstanceSpec
	for _, node := range nodeInstances {
		for _, inst := range node.Instances {
			if fallback == nil {
				fallback = inst
			}
			if inst.HostID == serviceHostID {
				return fmt.Sprintf("postgres-%s", inst.InstanceID), internalPort, nil
			}
		}
	}

	if fallback != nil {
		return fmt.Sprintf("postgres-%s", fallback.InstanceID), internalPort, nil
	}

	return "", 0, fmt.Errorf("no postgres instances found for service host %s", serviceHostID)
}
