package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/host"
)

var ErrDatabaseAlreadyExists = errors.New("database already exists")

type Service struct {
	orchestrator Orchestrator
	store        *Store
	hostSvc      *host.Service
}

func NewService(orchestrator Orchestrator, store *Store, hostSvc *host.Service) *Service {
	return &Service{
		orchestrator: orchestrator,
		store:        store,
		hostSvc:      hostSvc,
	}
}

func (s *Service) CreateDatabase(ctx context.Context, spec *Spec) (*Database, error) {
	if spec.DatabaseID == uuid.Nil {
		spec.DatabaseID = uuid.New()
	}
	specExists, err := s.store.Spec.ExistsByKey(spec.DatabaseID).Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check if database spec exists: %w", err)
	} else if specExists {
		return nil, ErrDatabaseAlreadyExists
	}

	var hostIDs []uuid.UUID
	// First pass to build out hostID list
	for _, node := range spec.Nodes {
		hostIDs = append(hostIDs, node.HostID)
		for _, replica := range node.ReadReplicas {
			hostIDs = append(hostIDs, replica.HostID)
		}
	}
	hosts, err := s.hostSvc.GetHosts(ctx, hostIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get hosts: %w", err)
	}
	defaultVersion, err := host.GreatestCommonDefaultVersion(hosts...)
	if err != nil {
		return nil, fmt.Errorf("unable to find greatest common default version among specified hosts: %w", err)
	}
	if spec.PostgresVersion == "" {
		spec.PostgresVersion = defaultVersion.PostgresVersion.String()
	}
	if spec.SpockVersion == "" {
		spec.SpockVersion = defaultVersion.SpockVersion.String()
	}
	specVersion, err := host.NewPgEdgeVersion(spec.PostgresVersion, spec.SpockVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to parse versions from spec: %w", err)
	}
	// Validate spec version and build up hosts by ID map for node validation
	hostsByID := map[uuid.UUID]*host.Host{}
	for _, h := range hosts {
		hostsByID[h.ID] = h
		if !h.Supports(specVersion) {
			return nil, fmt.Errorf("host %s does not support version combination: postgres=%s, spock=%s", h.ID, specVersion.PostgresVersion, specVersion.SpockVersion)
		}
	}
	// Second pass on nodes to validate node-level overrides
	for _, node := range spec.Nodes {
		h, ok := hostsByID[node.HostID]
		if !ok {
			return nil, fmt.Errorf("host %s not found in host list", node.HostID)
		}
		if node.PostgresVersion != "" {
			nodeVersion, err := host.NewPgEdgeVersion(node.PostgresVersion, spec.SpockVersion)
			if err != nil {
				return nil, fmt.Errorf("failed to parse versions from node %s spec: %w", node.Name, err)
			}
			if !h.Supports(nodeVersion) {
				return nil, fmt.Errorf("host %s does not support version combination: postgres=%s, spock=%s", h.ID, nodeVersion.PostgresVersion, nodeVersion.SpockVersion)
			}
		}
	}

	now := time.Now()
	db := &Database{
		DatabaseID: spec.DatabaseID,
		TenantID:   spec.TenantID,
		CreatedAt:  now,
		UpdatedAt:  now,
		State:      DatabaseStateCreating,
		Spec:       spec,
	}

	if err := s.store.Txn(
		s.store.Spec.Create(&StoredSpec{Spec: spec}),
		s.store.Database.Create(databaseToStored(db)),
	).Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to persist database: %w", err)
	}

	return db, nil
}

// func (s *Service) GetDatabase(ctx context.Context, databaseID uuid.UUID) (*Database, error) {

// 	storedSpec, err := s.store.Spec.GetByKey(databaseID).Exec(ctx)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to get database spec: %w", err)
// 	}

// 	return storedSpec.Spec, nil
// }
