package host

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/pgEdge/control-plane/server/internal/common"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/storage"
)

type Orchestrator interface {
	PopulateHost(ctx context.Context, h *Host) error
	PopulateHostStatus(ctx context.Context, h *HostStatus) error
}

type Service struct {
	cfg          config.Config
	etcd         *etcd.EmbeddedEtcd
	store        *Store
	orchestrator Orchestrator
}

func NewService(cfg config.Config, etcd *etcd.EmbeddedEtcd, store *Store, orchestrator Orchestrator) *Service {
	return &Service{
		cfg:          cfg,
		etcd:         etcd,
		store:        store,
		orchestrator: orchestrator,
	}
}

func (s *Service) UpdateHost(ctx context.Context) error {
	// resources, err := DetectResources()
	// if err != nil {
	// 	return fmt.Errorf("failed to detect system resources: %w", err)
	// }
	host := &Host{
		ID:           s.cfg.HostID,
		Orchestrator: s.cfg.Orchestrator,
		DataDir:      s.cfg.DataDir,
		Hostname:     s.cfg.Hostname,
		IPv4Address:  s.cfg.IPv4Address,
		// CPUs:         resources.CPUs,
		// MemBytes:     resources.MemBytes,
		// UpdatedAt: time.Now(),
		// Status: HostStatus{
		// 	State: HostStateHealthy,
		// 	Components: map[string]common.ComponentStatus{
		// 		"etcd": s.etcd.HealthCheck(),
		// 	},
		// },
	}
	if err := s.orchestrator.PopulateHost(ctx, host); err != nil {
		return fmt.Errorf("failed to populate orchestrator info: %w", err)
	}
	// Update host status based on component status
	// for _, component := range host.Status.Components {
	// 	if !component.Healthy {
	// 		host.Status.State = HostStateDegraded
	// 		break
	// 	}
	// }
	err := s.store.Host.
		Put(toStorage(host)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to persist host: %w", err)
	}

	return nil
}

func (s *Service) UpdateHostStatus(ctx context.Context) error {
	status := &HostStatus{
		HostID:    s.cfg.HostID,
		UpdatedAt: time.Now(),
		State:     HostStateHealthy,
		Components: map[string]common.ComponentStatus{
			"etcd": s.etcd.HealthCheck(),
		},
	}
	if err := s.orchestrator.PopulateHostStatus(ctx, status); err != nil {
		return fmt.Errorf("failed to populate orchestrator info: %w", err)
	}
	// Update host status based on component status
	for _, component := range status.Components {
		if !component.Healthy {
			status.State = HostStateDegraded
			break
		}
	}
	err := s.store.HostStatus.
		Put(statusToStorage(status)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to persist host status: %w", err)
	}

	return nil
}

func (s *Service) GetAllHosts(ctx context.Context) ([]*Host, error) {
	storedHosts, err := s.store.Host.
		GetAll().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch hosts from storage: %w", err)
	}
	storedStatuses, err := s.store.HostStatus.
		GetAll().
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch host statuses from storage: %w", err)
	}
	statusMap := make(map[string]*StoredHostStatus, len(storedStatuses))
	for _, status := range storedStatuses {
		statusMap[status.HostID] = status
	}

	hosts := make([]*Host, len(storedHosts))
	for idx, host := range storedHosts {
		status, ok := statusMap[host.ID]
		if !ok {
			status = &StoredHostStatus{
				HostID: host.ID,
				State:  HostStateUnknown,
			}
		}
		hosts[idx], err = fromStorage(host, status)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal host: %w", err)
		}
	}

	return hosts, nil
}

func (s *Service) GetHosts(ctx context.Context, hostIDs []string) ([]*Host, error) {
	storedHosts, err := s.store.Host.
		GetByKeys(hostIDs...).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch hosts from storage: %w", err)
	}
	storedStatuses, err := s.store.HostStatus.
		GetByKeys(hostIDs...).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch host statuses from storage: %w", err)
	}
	statusMap := make(map[string]*StoredHostStatus, len(storedStatuses))
	for _, status := range storedStatuses {
		statusMap[status.HostID] = status
	}

	hosts := make([]*Host, len(storedHosts))
	for idx, host := range storedHosts {
		status, ok := statusMap[host.ID]
		if !ok {
			status = &StoredHostStatus{
				HostID: host.ID,
				State:  HostStateUnknown,
			}
		}
		hosts[idx], err = fromStorage(host, status)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal host: %w", err)
		}
	}

	return hosts, nil
}

func (s *Service) GetHost(ctx context.Context, hostID string) (*Host, error) {
	storedHost, err := s.store.Host.
		GetByKey(hostID).
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch hosts from storage: %w", err)
	}
	storedStatus, err := s.store.HostStatus.
		GetByKey(hostID).
		Exec(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		storedStatus = &StoredHostStatus{
			HostID: hostID,
			State:  HostStateUnknown,
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch host statuses from storage: %w", err)
	}
	// statusMap := make(map[string]*StoredHostStatus, len(storedStatuses))
	// for _, status := range storedStatuses {
	// 	statusMap[status.HostID] = status
	// }

	host, err := fromStorage(storedHost, storedStatus)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal host: %w", err)
	}

	return host, nil
}

func (s *Service) RemoveHost(ctx context.Context, hostID string) error {
	_, err := s.store.Host.
		DeleteByKey(hostID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to remove host: %w", err)
	}
	_, err = s.store.HostStatus.
		DeleteByKey(hostID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to remove host status: %w", err)
	}

	return nil
}
