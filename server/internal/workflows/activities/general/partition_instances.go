package general

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/host"
)

type PartitionInstancesInput struct {
	Spec *database.Spec
}

type HostPartition struct {
	Host      *host.Host
	Instances []*database.InstanceSpec
}

type Cohort struct {
	Type     host.CohortType
	CohortID string
	Manager  *host.Host
}

type CohortPartition struct {
	Database *database.Spec
	Cohort   *Cohort
	Hosts    []*HostPartition
}

type PartitionInstancesOutput struct {
	Primaries []*CohortPartition
	Replicas  []*CohortPartition
}

func (a *Activities) PartitionInstances(ctx context.Context, input *PartitionInstancesInput) (*PartitionInstancesOutput, error) {
	nodes, err := input.Spec.NodeInstances()
	if err != nil {
		return nil, fmt.Errorf("failed to get instance specs: %w", err)
	}
	allHosts, err := a.HostService.GetAllHosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get all hosts: %w", err)
	}

	hostsByID := map[uuid.UUID]*host.Host{}
	managers := map[string]*host.Host{}
	for _, h := range allHosts {
		hostsByID[h.ID] = h
		if h.Cohort != nil && h.Cohort.ControlAvailable {
			// It's ok that this gets overwritten. Any manager will do.
			managers[string(h.Cohort.Type)+h.Cohort.CohortID] = h
		}
	}

	primaryHostPartitions := map[uuid.UUID]*HostPartition{}
	replicaHostPartitions := map[uuid.UUID]*HostPartition{}
	for _, node := range nodes {
		for idx, instance := range node.Instances {
			host, ok := hostsByID[instance.HostID]
			if !ok {
				return nil, fmt.Errorf("host %q not found", instance.HostID)
			}

			var partition *HostPartition
			if idx == 0 {
				// Elect the first host in the list as the primary.
				partition, ok = primaryHostPartitions[instance.HostID]
				if !ok {
					partition = &HostPartition{Host: host}
					primaryHostPartitions[instance.HostID] = partition
				}
			} else {
				partition, ok = replicaHostPartitions[instance.HostID]
				if !ok {
					partition = &HostPartition{Host: host}
					replicaHostPartitions[instance.HostID] = partition
				}
			}
			partition.Instances = append(partition.Instances, instance)
		}
	}

	primaries, err := groupByCohort(input.Spec, primaryHostPartitions, managers)
	if err != nil {
		return nil, fmt.Errorf("failed to group primary cohorts: %w", err)
	}
	replicas, err := groupByCohort(input.Spec, replicaHostPartitions, managers)
	if err != nil {
		return nil, fmt.Errorf("failed to group replica cohorts: %w", err)
	}

	return &PartitionInstancesOutput{
		Primaries: primaries,
		Replicas:  replicas,
	}, nil
}

func groupByCohort(spec *database.Spec, partitions map[uuid.UUID]*HostPartition, managers map[string]*host.Host) ([]*CohortPartition, error) {
	cohorts := map[string]*CohortPartition{}
	for id, partition := range partitions {
		cohort := partition.Host.Cohort
		if cohort == nil {
			cohorts[id.String()] = &CohortPartition{
				Database: spec,
				Cohort:   nil,
				Hosts:    []*HostPartition{partition},
			}
		} else {
			// Including type in the key to avoid collisions between different
			// cohort types
			cohortID := string(cohort.Type) + cohort.CohortID
			cp, ok := cohorts[cohortID]
			if !ok {
				manager, ok := managers[cohortID]
				if !ok {
					return nil, fmt.Errorf("no manager found for cohort %q", cohortID)
				}
				cp = &CohortPartition{
					Database: spec,
					Cohort: &Cohort{
						Type:     cohort.Type,
						CohortID: cohort.CohortID,
						Manager:  manager,
					},
					Hosts: []*HostPartition{partition},
				}
				cohorts[cohortID] = cp
			} else {
				cp.Hosts = append(cp.Hosts, partition)
			}
		}
	}

	out := make([]*CohortPartition, 0, len(cohorts))
	for _, p := range cohorts {
		out = append(out, p)
	}
	return out, nil
}
