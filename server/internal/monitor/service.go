package monitor

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/host"
)

var _ do.Shutdownable = (*Service)(nil)

type Service struct {
	appCtx           context.Context
	cfg              config.Config
	logger           zerolog.Logger
	dbSvc            *database.Service
	certSvc          *certificates.Service
	dbOrch           database.Orchestrator
	store            *Store
	hostMoniter      *HostMonitor
	instances        map[string]*InstanceMonitor
	serviceInstances map[string]*ServiceInstanceMonitor
}

func NewService(
	cfg config.Config,
	logger zerolog.Logger,
	dbSvc *database.Service,
	certSvc *certificates.Service,
	dbOrch database.Orchestrator,
	store *Store,
	hostSvc *host.Service,
) *Service {
	return &Service{
		cfg:              cfg,
		logger:           logger,
		dbSvc:            dbSvc,
		certSvc:          certSvc,
		dbOrch:           dbOrch,
		store:            store,
		instances:        map[string]*InstanceMonitor{},
		serviceInstances: map[string]*ServiceInstanceMonitor{},
		hostMoniter:      NewHostMonitor(logger, hostSvc),
	}
}

func (s *Service) Start(ctx context.Context) error {
	s.logger.Debug().Msg("starting monitors")

	// The monitors should run for the lifetime of the application rather than
	// the lifetime of a single operation.
	s.appCtx = ctx

	stored, err := s.store.InstanceMonitor.
		GetAllByHostID(s.cfg.HostID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to retrieve existing instance monitors: %w", err)
	}

	for _, inst := range stored {
		s.addInstanceMonitor(
			inst.DatabaseID,
			inst.InstanceID,
			inst.DatabaseName,
		)
	}

	storedSvcInst, err := s.store.ServiceInstanceMonitor.
		GetAllByHostID(s.cfg.HostID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to retrieve existing service instance monitors: %w", err)
	}

	for _, svcInst := range storedSvcInst {
		s.addServiceInstanceMonitor(
			svcInst.DatabaseID,
			svcInst.ServiceInstanceID,
			svcInst.HostID,
		)
	}

	return nil
}

func (s *Service) Shutdown() error {
	s.logger.Debug().Msg("shutting down monitors")

	for _, mon := range s.instances {
		mon.Stop()
	}

	s.instances = map[string]*InstanceMonitor{}

	for _, mon := range s.serviceInstances {
		mon.Stop()
	}

	s.serviceInstances = map[string]*ServiceInstanceMonitor{}

	return nil
}

func (s *Service) CreateInstanceMonitor(ctx context.Context, databaseID, instanceID, dbName string) error {
	if s.HasInstanceMonitor(instanceID) {
		err := s.DeleteInstanceMonitor(ctx, instanceID)
		if err != nil {
			return fmt.Errorf("failed to delete existing instance monitor: %w", err)
		}
	}

	err := s.store.InstanceMonitor.Put(&StoredInstanceMonitor{
		HostID:       s.cfg.HostID,
		DatabaseID:   databaseID,
		InstanceID:   instanceID,
		DatabaseName: dbName,
	}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to persist instance monitor: %w", err)
	}

	s.addInstanceMonitor(databaseID, instanceID, dbName)

	return nil
}

func (s *Service) DeleteInstanceMonitor(ctx context.Context, instanceID string) error {
	mon, ok := s.instances[instanceID]
	if ok {
		mon.Stop()
		delete(s.instances, instanceID)
	}

	_, err := s.store.InstanceMonitor.
		DeleteByKey(s.cfg.HostID, instanceID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete instance monitor: %w", err)
	}

	return nil
}

func (s *Service) HasInstanceMonitor(instanceID string) bool {
	_, ok := s.instances[instanceID]
	return ok
}

func (s *Service) addInstanceMonitor(databaseID, instanceID, dbName string) {
	mon := NewInstanceMonitor(
		s.dbOrch,
		s.dbSvc,
		s.certSvc,
		s.logger,
		databaseID,
		instanceID,
		dbName,
	)
	mon.Start(s.appCtx)
	s.instances[instanceID] = mon
}

func (s *Service) CreateServiceInstanceMonitor(ctx context.Context, databaseID, serviceInstanceID string) error {
	if s.HasServiceInstanceMonitor(serviceInstanceID) {
		err := s.DeleteServiceInstanceMonitor(ctx, serviceInstanceID)
		if err != nil {
			return fmt.Errorf("failed to delete existing service instance monitor: %w", err)
		}
	}

	err := s.store.ServiceInstanceMonitor.Put(&StoredServiceInstanceMonitor{
		HostID:            s.cfg.HostID,
		DatabaseID:        databaseID,
		ServiceInstanceID: serviceInstanceID,
	}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to persist service instance monitor: %w", err)
	}

	s.addServiceInstanceMonitor(databaseID, serviceInstanceID, s.cfg.HostID)

	return nil
}

func (s *Service) DeleteServiceInstanceMonitor(ctx context.Context, serviceInstanceID string) error {
	mon, ok := s.serviceInstances[serviceInstanceID]
	if ok {
		mon.Stop()
		delete(s.serviceInstances, serviceInstanceID)
	}

	_, err := s.store.ServiceInstanceMonitor.
		DeleteByKey(s.cfg.HostID, serviceInstanceID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete service instance monitor: %w", err)
	}

	return nil
}

func (s *Service) HasServiceInstanceMonitor(serviceInstanceID string) bool {
	_, ok := s.serviceInstances[serviceInstanceID]
	return ok
}

func (s *Service) addServiceInstanceMonitor(databaseID, serviceInstanceID, hostID string) {
	mon := NewServiceInstanceMonitor(
		s.dbOrch,
		s.dbSvc,
		s.logger,
		databaseID,
		serviceInstanceID,
		hostID,
	)
	mon.Start(s.appCtx)
	s.serviceInstances[serviceInstanceID] = mon
}
