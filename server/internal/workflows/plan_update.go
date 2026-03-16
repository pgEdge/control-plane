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
	// Use first node as canonical node for ServiceUserRole credential generation.
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

	// Build ordered host list for multi-host database connections.
	targetSessionAttrs := resolveTargetSessionAttrs(serviceSpec)

	var targetNodes []string
	if serviceSpec.DatabaseConnection != nil {
		targetNodes = serviceSpec.DatabaseConnection.TargetNodes
	}

	connInfo, err := database.BuildServiceHostList(&database.BuildServiceHostListParams{
		ServiceHostID:      hostID,
		NodeInstances:      nodeInstances,
		TargetNodes:        targetNodes,
		TargetSessionAttrs: targetSessionAttrs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build service host list: %w", err)
	}

	serviceInstanceSpec := &database.ServiceInstanceSpec{
		ServiceInstanceID:  serviceInstanceID,
		ServiceSpec:        serviceSpec,
		PgEdgeVersion:      pgEdgeVersion,
		DatabaseID:         spec.DatabaseID,
		DatabaseName:       spec.DatabaseName,
		HostID:             hostID,
		NodeName:           nodeName,
		DatabaseNetworkID:  database.GenerateDatabaseNetworkID(spec.DatabaseID),
		DatabaseHosts:      connInfo.Hosts,
		TargetSessionAttrs: connInfo.TargetSessionAttrs,
		Port:               serviceSpec.Port,
		DatabaseNodes:      nodeInstances,
		// Credentials: nil — ServiceUserRole.Create() will generate them
	}

	generateInput := &activities.GenerateServiceInstanceResourcesInput{Spec: serviceInstanceSpec}
	generateOutput, err := w.Activities.ExecuteGenerateServiceInstanceResources(ctx, generateInput).Get(ctx)
	if err != nil {
		return nil, err
	}

	svcResources := &operations.ServiceResources{
		ServiceInstanceID: serviceInstanceID,
		Resources:         generateOutput.Resources.Resources,
	}
	// Only attach the monitor when the service deploys a Docker container
	// (swarm.service_instance). Service types that provision infrastructure
	// without a container (e.g. "rag" in its initial phase) must not set this
	// dependency, as the planner requires all declared dependencies to exist.
	if serviceSpec.ServiceType != "rag" {
		svcResources.MonitorResource = &monitor.ServiceInstanceMonitorResource{
			DatabaseID:        spec.DatabaseID,
			ServiceInstanceID: serviceInstanceID,
			HostID:            hostID,
		}
	}
	return svcResources, nil
}

// resolveTargetSessionAttrs determines the target_session_attrs value for a
// service. Explicit user setting wins; otherwise each service type maps its
// own config semantics to the appropriate libpq value.
func resolveTargetSessionAttrs(serviceSpec *database.ServiceSpec) string {
	// Tier 1: Explicit user setting in database_connection
	if serviceSpec.DatabaseConnection != nil && serviceSpec.DatabaseConnection.TargetSessionAttrs != "" {
		return serviceSpec.DatabaseConnection.TargetSessionAttrs
	}
	// Tier 2: Per-service-type default
	switch serviceSpec.ServiceType {
	case "mcp":
		// MCP maps allow_writes → primary/prefer-standby
		if allowWrites, ok := serviceSpec.Config["allow_writes"].(bool); ok && allowWrites {
			return database.TargetSessionAttrsPrimary
		}
		return database.TargetSessionAttrsPreferStandby
	// Future service types add cases here.
	default:
		// Default to "prefer-standby" for safety — read-only unless the
		// service explicitly opts in to writes.
		return database.TargetSessionAttrsPreferStandby
	}
}
