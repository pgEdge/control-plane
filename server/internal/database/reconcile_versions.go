package database

import (
	"context"
	"errors"
	"fmt"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/storage"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

func (s *Service) ReconcileAllDatabaseVersions(ctx context.Context) error {
	databases, err := s.store.Database.GetAll().Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to get databases: %w", err)
	}
	for _, database := range databases {
		if database.State.IsInProgress() {
			continue
		}
		spec, err := s.store.Spec.
			GetByKey(database.DatabaseID).
			Exec(ctx)
		if errors.Is(err, storage.ErrNotFound) {
			continue
		} else if err != nil {
			return fmt.Errorf("failed to get spec for database '%s': %w", database.DatabaseID, err)
		}
		instances, err := s.store.Instance.
			GetByDatabaseID(database.DatabaseID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to get instances for database '%s': %w", database.DatabaseID, err)
		}
		instanceStatuses, err := s.store.InstanceStatus.
			GetByDatabaseID(database.DatabaseID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to get instance statuses for database '%s': %w", database.DatabaseID, err)
		}

		logger := s.logger.With().
			Str("database_id", database.DatabaseID).
			Logger()

		var ops []storage.TxnOperation
		updatedSpec, updatedInstances := ReconcileVersions(spec, instances, instanceStatuses)
		for _, instance := range updatedInstances {
			logger.Info().
				Str("node_name", instance.NodeName).
				Str("host_id", instance.HostID).
				Str("instance_id", instance.InstanceID).
				Stringer("postgres_version", instance.PgEdgeVersion.PostgresVersion).
				Stringer("spock_version", instance.PgEdgeVersion.SpockVersion).
				Msg("detected updated instance version")

			instanceSpec, err := s.store.InstanceSpec.
				GetByKey(instance.DatabaseID, instance.InstanceID).
				Exec(ctx)
			if err != nil && !errors.Is(err, storage.ErrNotFound) {
				return fmt.Errorf("failed to get instance spec for instance '%s': %w", instance.InstanceID, err)
			} else if err == nil {
				instanceSpec.Spec.PgEdgeVersion = instance.PgEdgeVersion
				ops = append(ops, s.store.InstanceSpec.Update(instanceSpec))
			}

			ops = append(ops, s.store.Instance.Update(instance))
		}
		if updatedSpec != nil {
			logger.Info().Msg("detected updated node versions")
			ops = append(ops, s.store.Spec.Update(updatedSpec))
		}
		if len(ops) == 0 {
			continue
		}

		// We want to abandon this update if the database has been updated since
		// we last fetched it.
		databaseNotUpdated := clientv3.Compare(clientv3.Version(s.store.Database.Key(database.DatabaseID)), "=", database.Version())
		txn := s.store.Txn(ops...)
		txn.AddConditions(databaseNotUpdated)
		err = txn.Commit(ctx)
		switch {
		case errors.Is(err, storage.ErrOperationConstraintViolated):
			logger.Warn().Msg("database modified while updating detected version. skipping update.")
		case err != nil:
			return fmt.Errorf("failed to update records for database '%s': %w", database.DatabaseID, err)
		default:
			logger.Info().Msg("successfully updated with detected versions")
		}
	}

	return nil
}

func ReconcileVersions(
	spec *StoredSpec,
	instances []*StoredInstance,
	statuses []*StoredInstanceStatus,
) (*StoredSpec, []*StoredInstance) {
	instancesByNodeHost, updatedInstances := reconcileInstanceVersions(instances, statuses)
	updatedSpec := reconcileNodeVersions(spec, instancesByNodeHost)

	return updatedSpec, updatedInstances
}

type nodeHostKey struct {
	nodeName string
	hostID   string
}

func reconcileInstanceVersions(
	instances []*StoredInstance,
	statuses []*StoredInstanceStatus,
) (map[nodeHostKey]*StoredInstance, []*StoredInstance) {
	var updatedInstances []*StoredInstance
	statusesByID := make(map[string]*StoredInstanceStatus, len(statuses))
	for _, status := range statuses {
		statusesByID[status.InstanceID] = status
	}
	instancesByNodeHost := make(map[nodeHostKey]*StoredInstance, len(instances))
	for _, instance := range instances {
		status, ok := statusesByID[instance.InstanceID]
		if !ok || status.Status.IsStale() {
			continue
		}
		postgresVersion := utils.FromPointer(status.Status.PostgresVersion)
		spockVersion := utils.FromPointer(status.Status.SpockVersion)
		pgEdgeVersion, err := ds.ParsePgEdgeVersion(postgresVersion, spockVersion)
		if err != nil {
			continue
		}
		pgEdgeVersion, err = pgEdgeVersion.Normalize()
		if err != nil {
			continue
		}
		if !instance.PgEdgeVersion.Equals(pgEdgeVersion) {
			instance.PgEdgeVersion = pgEdgeVersion
			updatedInstances = append(updatedInstances, instance)
		}
		instancesByNodeHost[nodeHostKey{instance.NodeName, instance.HostID}] = instance
	}

	return instancesByNodeHost, updatedInstances
}

func observedNodeVersion(
	node *Node,
	instancesByNodeHost map[nodeHostKey]*StoredInstance,
) *ds.PgEdgeVersion {
	var version *ds.PgEdgeVersion
	for _, hostID := range node.HostIDs {
		instance, ok := instancesByNodeHost[nodeHostKey{nodeName: node.Name, hostID: hostID}]
		switch {
		case !ok:
			return nil
		case version == nil:
			version = instance.PgEdgeVersion
		case !version.Equals(instance.PgEdgeVersion):
			return nil
		}
	}
	return version
}

func reconcileNodeVersions(
	spec *StoredSpec,
	instancesByNodeHost map[nodeHostKey]*StoredInstance,
) *StoredSpec {
	var updatedSpec *StoredSpec
	var commonSpockVersion string
	spockMatches := true
	for _, node := range spec.Nodes {
		currentPostgresVersion := node.PostgresVersion
		if currentPostgresVersion == "" {
			currentPostgresVersion = spec.PostgresVersion
		}
		observed := observedNodeVersion(node, instancesByNodeHost)
		if observed == nil {
			// we only want to update our spock version when _all_ nodes are
			// observed to have the same spock version
			spockMatches = false
			continue
		}
		observedPostgresVersion := observed.PostgresVersion.String()
		observedSpockVersion := observed.SpockVersion.String()

		if observedPostgresVersion != currentPostgresVersion {
			node.PostgresVersion = observedPostgresVersion
			// signals that we've modified the spec
			updatedSpec = spec
		}
		if commonSpockVersion == "" {
			commonSpockVersion = observedSpockVersion
		} else if commonSpockVersion != observedSpockVersion {
			spockMatches = false
		}
	}
	if spockMatches && commonSpockVersion != "" && commonSpockVersion != spec.SpockVersion {
		spec.SpockVersion = commonSpockVersion
		updatedSpec = spec
	}
	if updatedSpec != nil {
		updatedSpec.NormalizePostgresVersions()
		if spockMatches && commonSpockVersion != "" {
			updatedSpec.SpockVersion = commonSpockVersion
		}
	}

	return updatedSpec
}
