package host

import (
	"time"

	"github.com/pgEdge/control-plane/server/internal/common"
	"github.com/pgEdge/control-plane/server/internal/config"
)

type HostState string

const (
	HostStateHealthy     HostState = "healthy"
	HostStateUnreachable HostState = "unreachable"
	HostStateDegraded    HostState = "degraded"
	HostStateUnknown     HostState = "unknown"
)

type HostStatus struct {
	HostID     string
	UpdatedAt  time.Time
	State      HostState
	Components map[string]common.ComponentStatus
}

type CohortType string

const (
	CohortTypeSwarm CohortType = "swarm"
)

type Cohort struct {
	Type             CohortType
	MemberID         string
	ControlAvailable bool
}

type Host struct {
	ID                      string
	Orchestrator            config.Orchestrator
	Cohort                  *Cohort
	DataDir                 string
	Hostname                string
	IPv4Address             string
	CPUs                    int
	MemBytes                uint64
	Status                  *HostStatus
	DefaultPgEdgeVersion    *PgEdgeVersion
	SupportedPgEdgeVersions []*PgEdgeVersion
}

func (h *Host) Supports(pgEdgeVersion *PgEdgeVersion) bool {
	for _, v := range h.SupportedPgEdgeVersions {
		if v.Equals(pgEdgeVersion) {
			return true
		}
	}
	return false
}

func fromStorage(host *StoredHost, status *StoredHostStatus) (*Host, error) {
	// defaultPostgresVersion, err := semver.NewVersion(host.DefaultPostgresVersion)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to unmarshal default postgres version: %w", err)
	// }
	// defaultSpockVersion, err := semver.NewVersion(host.DefaultSpockVersion)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to unmarshal default spock version: %w", err)
	// }
	// supportedVersions := ds.NewSet[PgEdgeVersion]()
	// for pgVersion, spockVersions := range host.SupportedVersions {
	// 	for spockVersion := range spockVersions {
	// 		pgEdgeVersion, err := pgEdgeVersionFromStrings(pgVersion, spockVersion)
	// 		if err != nil {
	// 			return nil, fmt.Errorf("failed to unmarshal supported versions: %w", err)
	// 		}
	// 		supportedVersions.Add(pgEdgeVersion)
	// 	}
	// }

	var cohort *Cohort
	if host.Cohort != nil {
		cohort = &Cohort{
			Type:             host.Cohort.Type,
			MemberID:         host.Cohort.MemberID,
			ControlAvailable: host.Cohort.ControlAvailable,
		}
	}
	out := &Host{
		ID:                      host.ID,
		Orchestrator:            host.Orchestrator,
		Cohort:                  cohort,
		DataDir:                 host.DataDir,
		Hostname:                host.Hostname,
		IPv4Address:             host.IPv4Address,
		CPUs:                    host.CPUs,
		MemBytes:                host.MemBytes,
		SupportedPgEdgeVersions: host.SupportedPgEdgeVersions,
		DefaultPgEdgeVersion:    host.DefaultPgEdgeVersion,
		Status: &HostStatus{
			UpdatedAt:  status.UpdatedAt,
			State:      status.State,
			Components: status.Components,
		},
	}

	// Host is considered unreachable if it has failed to check in for 2
	// heartbeats.
	if time.Since(out.Status.UpdatedAt) > 2*UpdateStatusInterval {
		out.Status.State = HostStateUnreachable
		out.Status.Components = nil //Clear stale component statuses
	}

	return out, nil
}

func toStorage(host *Host) *StoredHost {
	var cohort *StoredCohort
	if host.Cohort != nil {
		cohort = &StoredCohort{
			Type:             host.Cohort.Type,
			MemberID:         host.Cohort.MemberID,
			ControlAvailable: host.Cohort.ControlAvailable,
		}
	}

	// supportedVersions := map[string]map[string]bool{}
	// for pgEdgeVersion := range host.SupportedVersions {
	// 	pgV := pgEdgeVersion.PostgresVersion.String()
	// 	spockV := pgEdgeVersion.SpockVersion.String()
	// 	if _, ok := supportedVersions[pgV]; !ok {
	// 		supportedVersions[pgV] = map[string]bool{}
	// 	}
	// 	supportedVersions[pgV][spockV] = true
	// }

	return &StoredHost{
		ID:                      host.ID,
		Orchestrator:            host.Orchestrator,
		Cohort:                  cohort,
		DataDir:                 host.DataDir,
		Hostname:                host.Hostname,
		IPv4Address:             host.IPv4Address,
		CPUs:                    host.CPUs,
		MemBytes:                host.MemBytes,
		DefaultPgEdgeVersion:    host.DefaultPgEdgeVersion,
		SupportedPgEdgeVersions: host.SupportedPgEdgeVersions,
	}
}

func statusToStorage(status *HostStatus) *StoredHostStatus {
	return &StoredHostStatus{
		HostID:     status.HostID,
		UpdatedAt:  status.UpdatedAt,
		State:      status.State,
		Components: status.Components,
	}
}
