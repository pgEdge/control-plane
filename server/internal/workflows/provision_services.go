package workflows

import (
	"fmt"

	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/monitor"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/utils"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities"
)

type ProvisionServicesInput struct {
	TaskID uuid.UUID      `json:"task_id"`
	Spec   *database.Spec `json:"spec"`
}

type ProvisionServicesOutput struct {
}

func (w *Workflows) ExecuteProvisionServices(
	ctx workflow.Context,
	input *ProvisionServicesInput,
) workflow.Future[*ProvisionServicesOutput] {
	options := workflow.SubWorkflowOptions{
		Queue: utils.HostQueue(w.Config.HostID),
		RetryOptions: workflow.RetryOptions{
			MaxAttempts: 1,
		},
	}
	return workflow.CreateSubWorkflowInstance[*ProvisionServicesOutput](ctx, options, w.ProvisionServices, input)
}

func (w *Workflows) ProvisionServices(ctx workflow.Context, input *ProvisionServicesInput) (*ProvisionServicesOutput, error) {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID)
	logger.With("service_count", len(input.Spec.Services)).Info("ProvisionServices workflow started")

	if len(input.Spec.Services) == 0 {
		logger.Info("no services to provision - returning early")
		return &ProvisionServicesOutput{}, nil
	}

	// Parse database version for service compatibility validation
	pgEdgeVersion, err := host.NewPgEdgeVersion(input.Spec.PostgresVersion, input.Spec.SpockVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pgedge version: %w", err)
	}

	// Log task start
	start := workflow.Now(ctx)
	err = w.logTaskEvent(ctx,
		task.ScopeDatabase,
		input.Spec.DatabaseID,
		input.TaskID,
		task.LogEntry{
			Message: fmt.Sprintf("provisioning %d service(s)", len(input.Spec.Services)),
			Fields: map[string]any{
				"service_count": len(input.Spec.Services),
			},
		},
	)
	if err != nil {
		return nil, err
	}

	// Get existing database state
	getCurrentInput := &activities.GetCurrentStateInput{
		DatabaseID: input.Spec.DatabaseID,
	}
	getCurrentOutput, err := w.Activities.ExecuteGetCurrentState(ctx, getCurrentInput).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current state: %w", err)
	}
	accumulatedState := getCurrentOutput.State

	// Track prepared service instances for parallel deployment
	type serviceInstancePrep struct {
		serviceInstanceID string
		serviceSpec       *database.ServiceSpec
		hostID            string
		resources         []*resource.ResourceData
		logFields         []any // Logger fields for this instance
	}
	var preparedInstances []serviceInstancePrep

	// Phase 1: Prepare all service instances (create users, generate resources)
	// This must be done serially because CreateServiceUser connects to Postgres
	for _, serviceSpec := range input.Spec.Services {
		logger := logger.With("service_id", serviceSpec.ServiceID)

		// Log service provisioning start
		err := w.logTaskEvent(ctx,
			task.ScopeDatabase,
			input.Spec.DatabaseID,
			input.TaskID,
			task.LogEntry{
				Message: fmt.Sprintf("provisioning service '%s' on %d host(s)", serviceSpec.ServiceID, len(serviceSpec.HostIDs)),
				Fields: map[string]any{
					"service_id":   serviceSpec.ServiceID,
					"service_type": serviceSpec.ServiceType,
					"version":      serviceSpec.Version,
					"host_count":   len(serviceSpec.HostIDs),
				},
			},
		)
		if err != nil {
			return nil, err
		}

		// Prepare each service instance on each host
		for _, hostID := range serviceSpec.HostIDs {
			serviceInstanceID := database.GenerateServiceInstanceID(input.Spec.DatabaseID, serviceSpec.ServiceID, hostID)
			instanceLogger := logger.With("service_instance_id", serviceInstanceID, "host_id", hostID)

			// Store service instance immediately with state="creating"
			// This ensures it's visible even if provisioning fails later
			storeInitialInput := &activities.StoreServiceInstanceInput{
				ServiceInstance: &database.ServiceInstance{
					ServiceInstanceID: serviceInstanceID,
					ServiceID:         serviceSpec.ServiceID,
					DatabaseID:        input.Spec.DatabaseID,
					HostID:            hostID,
					State:             database.ServiceInstanceStateCreating,
					CreatedAt:         workflow.Now(ctx),
					UpdatedAt:         workflow.Now(ctx),
				},
			}
			_, err := w.Activities.ExecuteStoreServiceInstance(ctx, storeInitialInput).Get(ctx)
			if err != nil {
				instanceLogger.With("error", err).Error("failed to store initial service instance")
				return nil, fmt.Errorf("failed to store service instance %s: %w", serviceInstanceID, err)
			}

			instanceLogger.Info("stored service instance with state=creating")

			// Error handler to mark service instance as failed and continue to next instance
			// Can only be used AFTER the service instance is stored above
			handleError := func(cause error) {
				instanceLogger.With("error", cause).Error("failed to prepare service instance")

				// Mark service instance as failed
				updateInstanceInput := &activities.UpdateServiceInstanceStateInput{
					ServiceInstanceID: serviceInstanceID,
					DatabaseID:        input.Spec.DatabaseID,
					State:             database.ServiceInstanceStateFailed,
					Error:             cause.Error(),
				}
				_, stateErr := w.Activities.ExecuteUpdateServiceInstanceState(ctx, updateInstanceInput).Get(ctx)
				if stateErr != nil {
					instanceLogger.With("error", stateErr).Warn("failed to update service instance state to failed")
				}
			}

			// Find any Postgres instance in this database to get connection details
			// Services can be on different hosts than database instances, they just need
			// database network connectivity. We prefer an instance on the same host for
			// lower latency, but any instance will work.
			var instanceHostname string
			var instancePort = 5432   // Default Postgres port
			var instanceHostID string // Host where the instance is running (needed for CreateServiceUser)
			instanceResources := accumulatedState.GetAll(database.ResourceTypeInstance)

			// First try to find an instance on the same host (preferred for latency)
			for _, instanceData := range instanceResources {
				instance, err := resource.ToResource[*database.InstanceResource](instanceData)
				if err != nil {
					continue
				}
				if instance.Spec.DatabaseID == input.Spec.DatabaseID && instance.Spec.HostID == hostID {
					instanceHostname = instance.InstanceHostname
					instanceHostID = instance.Spec.HostID
					if instance.Spec.Port != nil {
						instancePort = *instance.Spec.Port
					}
					break
				}
			}

			// If no instance on same host, use any instance in the database
			if instanceHostname == "" {
				for _, instanceData := range instanceResources {
					instance, err := resource.ToResource[*database.InstanceResource](instanceData)
					if err != nil {
						continue
					}
					if instance.Spec.DatabaseID == input.Spec.DatabaseID {
						instanceHostname = instance.InstanceHostname
						instanceHostID = instance.Spec.HostID
						if instance.Spec.Port != nil {
							instancePort = *instance.Spec.Port
						}
						break
					}
				}
			}

			if instanceHostname == "" {
				handleError(fmt.Errorf("no postgres instance found for database %s", input.Spec.DatabaseID))
				continue
			}

			// Create database credentials for this service instance
			// IMPORTANT: Execute on instanceHostID (where Postgres is running), not hostID (where service will run)
			// CreateServiceUser needs to connect to the local Postgres container via Docker
			createUserInput := &activities.CreateServiceUserInput{
				DatabaseID:        input.Spec.DatabaseID,
				DatabaseName:      input.Spec.DatabaseName,
				ServiceInstanceID: serviceInstanceID,
				ServiceID:         serviceSpec.ServiceID,
				HostID:            hostID,
			}
			createUserOutput, err := w.Activities.ExecuteCreateServiceUser(ctx, instanceHostID, createUserInput).Get(ctx)
			if err != nil {
				handleError(fmt.Errorf("failed to create service user for instance %s: %w", serviceInstanceID, err))
				continue
			}

			instanceLogger.With("username", createUserOutput.Credentials.Username).Info("created service instance credentials")

			// Generate service instance resources
			// Note: CohortMemberID is populated by the orchestrator using its swarmNodeID
			serviceInstanceSpec := &database.ServiceInstanceSpec{
				ServiceInstanceID: serviceInstanceID,
				ServiceSpec:       serviceSpec,
				PgEdgeVersion:     pgEdgeVersion,
				DatabaseID:        input.Spec.DatabaseID,
				DatabaseName:      input.Spec.DatabaseName,
				HostID:            hostID,
				ServiceName:       database.GenerateServiceName(serviceSpec.ServiceType, input.Spec.DatabaseID, serviceSpec.ServiceID, hostID),
				Hostname:          database.GenerateServiceName(serviceSpec.ServiceType, input.Spec.DatabaseID, serviceSpec.ServiceID, hostID),
				Credentials:       createUserOutput.Credentials,
				DatabaseNetworkID: database.GenerateDatabaseNetworkID(input.Spec.DatabaseID),
				DatabaseHost:      instanceHostname,
				DatabasePort:      instancePort,
				Port:              serviceSpec.Port,
			}

			generateInput := &activities.GenerateServiceInstanceResourcesInput{
				Spec: serviceInstanceSpec,
			}
			generateOutput, err := w.Activities.ExecuteGenerateServiceInstanceResources(ctx, generateInput).Get(ctx)
			if err != nil {
				handleError(fmt.Errorf("failed to generate service instance resources: %w", err))
				continue
			}

			instanceLogger.With("resource_count", len(generateOutput.Resources.Resources)).Info("generated service instance resources")

			// Add monitor resource to track service instance state transitions
			monitorResource := &monitor.ServiceInstanceMonitorResource{
				DatabaseID:        input.Spec.DatabaseID,
				ServiceInstanceID: serviceInstanceID,
				HostID:            hostID,
			}
			monitorResourceData, err := resource.ToResourceData(monitorResource)
			if err != nil {
				handleError(fmt.Errorf("failed to convert monitor resource to resource data: %w", err))
				continue
			}
			generateOutput.Resources.Resources = append(generateOutput.Resources.Resources, monitorResourceData)

			instanceLogger.With("resource_count", len(generateOutput.Resources.Resources)).Info("generated service instance resources with monitor")

			// Add to prepared instances for parallel deployment
			preparedInstances = append(preparedInstances, serviceInstancePrep{
				serviceInstanceID: serviceInstanceID,
				serviceSpec:       serviceSpec,
				hostID:            hostID,
				resources:         generateOutput.Resources.Resources,
				logFields:         []any{"service_instance_id", serviceInstanceID, "host_id", hostID},
			})
		}

	}

	// Phase 2 & 3: Deploy all service instances in parallel
	if len(preparedInstances) > 0 {
		// Accumulate all service instance resources into desired state
		serviceDesiredState := resource.NewState()
		for _, prep := range preparedInstances {
			serviceDesiredState.Add(prep.resources...)
		}

		// Plan all service resources together (shares network, etc.)
		serviceCurrentState := resource.NewState()
		servicePlan, err := serviceCurrentState.Plan(resource.PlanOptions{}, serviceDesiredState)
		if err != nil {
			return nil, fmt.Errorf("failed to plan service instance resources: %w", err)
		}

		// Apply all service resources in parallel (same pattern as database instances)
		err = w.applyEvents(ctx, input.Spec.DatabaseID, input.TaskID, serviceCurrentState, servicePlan)
		if err != nil {
			logger.With("error", err).Warn("some service instances failed to deploy")

			// Check for resource errors and mark service instances as failed
			// applyEvents may have aborted before creating monitors, so we must handle state transitions here
			for _, prep := range preparedInstances {
				// Get the ServiceInstance resource from current state to check for errors
				// Use the same identifier format as swarm.ServiceInstanceResourceIdentifier but without import cycle
				serviceInstanceIdentifier := resource.Identifier{
					ID:   prep.serviceInstanceID,
					Type: resource.Type(database.ResourceTypeServiceInstance),
				}
				serviceInstanceResourceData, found := serviceCurrentState.Get(serviceInstanceIdentifier)

				if found && serviceInstanceResourceData != nil && serviceInstanceResourceData.Error != "" {
					// ServiceInstance deployment failed - mark as failed in etcd
					updateInput := &activities.UpdateServiceInstanceStateInput{
						ServiceInstanceID: prep.serviceInstanceID,
						DatabaseID:        input.Spec.DatabaseID,
						State:             database.ServiceInstanceStateFailed,
						Error:             serviceInstanceResourceData.Error,
					}

					_, updateErr := w.Activities.ExecuteUpdateServiceInstanceState(ctx, updateInput).Get(ctx)
					if updateErr != nil {
						logger.With("error", updateErr, "service_instance_id", prep.serviceInstanceID).
							Error("failed to update service instance state to failed")
					} else {
						logger.With("service_instance_id", prep.serviceInstanceID).
							Info("marked service instance as failed due to deployment error")
					}
				}
			}
		}

		// Merge service resources into accumulated state
		for _, resourcesByID := range serviceCurrentState.Resources {
			for _, res := range resourcesByID {
				accumulatedState.Add(res)
			}
		}

		// Phase 4: Update statuses for all service instances
		for _, prep := range preparedInstances {
			instanceLogger := logger.With(prep.logFields...)

			// Get service instance status (connection info, ports)
			statusInput := &activities.GetServiceInstanceStatusInput{
				ServiceInstanceID: prep.serviceInstanceID,
				HostID:            prep.hostID,
			}
			statusOutput, err := w.Activities.ExecuteGetServiceInstanceStatus(ctx, prep.hostID, statusInput).Get(ctx)
			if err != nil {
				instanceLogger.With("error", err).Warn("failed to get service instance status (monitor will enrich)")
				continue
			}

			// If status is nil, the container is still starting - leave state as "creating"
			// The instance monitor will update it to "running" once the container is ready
			if statusOutput.Status == nil {
				instanceLogger.Info("service container still starting - status will be populated by monitoring")
				continue
			}

			// Update service instance state to "running" with connection info
			updateInstanceInput := &activities.UpdateServiceInstanceStateInput{
				ServiceInstanceID: prep.serviceInstanceID,
				DatabaseID:        input.Spec.DatabaseID,
				State:             database.ServiceInstanceStateRunning,
				Status:            statusOutput.Status,
			}
			_, err = w.Activities.ExecuteUpdateServiceInstanceState(ctx, updateInstanceInput).Get(ctx)
			if err != nil {
				instanceLogger.With("error", err).Error("failed to update service instance state")
				continue
			}

			instanceLogger.Info("service instance provisioned successfully")
		}

	}

	// Log overall service provisioning completion
	for _, serviceSpec := range input.Spec.Services {
		err = w.logTaskEvent(ctx,
			task.ScopeDatabase,
			input.Spec.DatabaseID,
			input.TaskID,
			task.LogEntry{
				Message: fmt.Sprintf("provisioned service '%s' on %d host(s)", serviceSpec.ServiceID, len(serviceSpec.HostIDs)),
				Fields: map[string]any{
					"service_id": serviceSpec.ServiceID,
					"host_count": len(serviceSpec.HostIDs),
				},
			},
		)
		if err != nil {
			return nil, err
		}
	}

	// Persist the complete state with all database and service instance resources
	persistInput := &activities.PersistStateInput{
		DatabaseID: input.Spec.DatabaseID,
		State:      accumulatedState,
	}
	_, err = w.Activities.ExecutePersistState(ctx, persistInput).Get(ctx)
	if err != nil {
		logger.With("error", err).Error("failed to persist service instance state")
		return nil, fmt.Errorf("failed to persist service instance state: %w", err)
	}

	// Log task completion
	duration := workflow.Now(ctx).Sub(start)
	err = w.logTaskEvent(ctx,
		task.ScopeDatabase,
		input.Spec.DatabaseID,
		input.TaskID,
		task.LogEntry{
			Message: fmt.Sprintf("finished provisioning %d service(s) (took %s)", len(input.Spec.Services), duration),
			Fields: map[string]any{
				"service_count": len(input.Spec.Services),
				"duration_ms":   duration.Milliseconds(),
			},
		},
	)
	if err != nil {
		return nil, err
	}

	logger.Info("successfully provisioned all services")

	return &ProvisionServicesOutput{}, nil
}
