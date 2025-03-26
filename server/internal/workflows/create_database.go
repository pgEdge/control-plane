package workflows

import (
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities/general"

	swarm_activities "github.com/pgEdge/control-plane/server/internal/workflows/activities/swarm"
	"github.com/pgEdge/control-plane/server/internal/workflows/swarm"
)

type CreateDatabaseInput struct {
	Spec *database.Spec
}

// TODO: Need to record failure in case of error

func (w *Workflows) CreateDatabase(ctx workflow.Context, input *CreateDatabaseInput) error {
	logger := workflow.Logger(ctx).With("database_id", input.Spec.DatabaseID)
	logger.Info("creating database")

	activityOptions := workflow.ActivityOptions{
		Queue: core.Queue(w.Config.HostID.String()), // Keep it on this host for simplicity
	}
	partitions, err := workflow.ExecuteActivity[*general.PartitionInstancesOutput](ctx,
		activityOptions,
		w.GeneralActivities.PartitionInstances,
		&general.PartitionInstancesInput{
			Spec: input.Spec,
		},
	).Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to partition instances: %w", err)
	}

	// Create primary instances first
	for _, partition := range partitions.Primaries {
		if partition.Cohort == nil {
			return fmt.Errorf("non-swarm hosts are not currently supported by this workflow")
		}
		switch partition.Cohort.Type {
		case host.CohortTypeSwarm:
			_, err := w.SwarmWorkflows.ExecuteCreateCohortInstances(ctx, partition.Cohort.Manager.ID, &swarm.CreateCohortInstancesInput{
				Partition:   partition,
				ClusterSize: len(input.Spec.Nodes),
			}).Get(ctx)
			if err != nil {
				return fmt.Errorf("failed to create instances: %w", err)
			}
		default:
			return fmt.Errorf("non-swarm hosts are not currently supported by this workflow")
		}
	}

	allPrimaries := []*database.InstanceSpec{}
	for _, partition := range partitions.Primaries {
		for _, hostPartition := range partition.Hosts {
			allPrimaries = append(allPrimaries, hostPartition.Instances...)
		}
	}

	// Crosswire the primary instances
	var futures []workflow.Future[*swarm_activities.InitializeDBOutput]
	for _, partition := range partitions.Primaries {
		if partition.Cohort == nil {
			return fmt.Errorf("non-swarm hosts are not currently supported by this workflow")
		}
		switch partition.Cohort.Type {
		case host.CohortTypeSwarm:
			for _, hostPartition := range partition.Hosts {
				for _, instance := range hostPartition.Instances {
					futures = append(futures, w.SwarmWorkflows.Swarm.ExecuteInitializeDB(ctx, hostPartition.Host.ID, &swarm_activities.InitializeDBInput{
						Instance:     instance,
						AllPrimaries: allPrimaries,
					}))
				}
			}
		default:
			return fmt.Errorf("non-swarm hosts are not currently supported by this workflow")
		}
	}

	var errs []error
	for _, future := range futures {
		if _, err := future.Get(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to initialize database: %w", err))
		}
	}

	if err := errors.Join(errs...); err != nil {
		logger.Error("failed to create database", "error", err)
		return err
	}

	logger.Info("database created successfully")

	return nil
}
