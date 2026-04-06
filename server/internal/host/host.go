package host

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/healthcheck"
)

const HostMonitorRefreshInterval = 15 * time.Second

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
	Components map[string]healthcheck.ComponentStatus
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
	PeerAddresses           []string
	ClientAddresses         []string
	CPUs                    int
	MemBytes                uint64
	EtcdMode                config.EtcdMode
	Status                  *HostStatus
	DefaultPgEdgeVersion    *ds.PgEdgeVersion
	SupportedPgEdgeVersions []*ds.PgEdgeVersion
}

func (h *Host) Supports(pgEdgeVersion *ds.PgEdgeVersion) bool {
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
		PeerAddresses:           host.PeerAddresses,
		ClientAddresses:         host.ClientAddresses,
		CPUs:                    host.CPUs,
		MemBytes:                host.MemBytes,
		EtcdMode:                host.EtcdMode,
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
	if time.Since(out.Status.UpdatedAt) > 2*HostMonitorRefreshInterval {
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
		PeerAddresses:           host.PeerAddresses,
		ClientAddresses:         host.ClientAddresses,
		CPUs:                    host.CPUs,
		MemBytes:                host.MemBytes,
		EtcdMode:                host.EtcdMode,
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

func GreatestCommonDefaultVersion(hosts ...*Host) (*ds.PgEdgeVersion, error) {
	// We can't do set operations on *PgEdgeVersion, and we can't do semver
	// comparisons on strings. So, we'll use strings for set operations, then
	// translate them back to *PgEdgeVersions to do the version comparisons.
	stringToVersion := map[string]*ds.PgEdgeVersion{}
	defaultVersions := ds.NewSet[string]()
	var commonVersions ds.Set[string]
	for _, h := range hosts {
		if h.DefaultPgEdgeVersion == nil {
			return nil, fmt.Errorf("missing default pgedge version on host '%s'", h.ID)
		}
		defaultVersions.Add(h.DefaultPgEdgeVersion.String())
		supported := ds.NewSet[string]()
		for _, v := range h.SupportedPgEdgeVersions {
			vs := v.String()
			supported.Add(vs)
			stringToVersion[vs] = v
		}
		if commonVersions == nil {
			commonVersions = supported
		} else {
			commonVersions = commonVersions.Intersection(supported)
		}
	}

	commonDefaults := defaultVersions.Intersection(commonVersions)
	if len(commonDefaults) == 0 {
		return nil, errors.New("no common default versions found between the given hosts")
	}

	versions := make([]*ds.PgEdgeVersion, 0, len(commonDefaults))
	for vs := range commonDefaults {
		v, ok := stringToVersion[vs]
		if !ok {
			return nil, fmt.Errorf("invalid state - missing version: %q", vs)
		}
		versions = append(versions, v)
	}
	slices.SortFunc(versions, func(a, b *ds.PgEdgeVersion) int {
		// Sort in reverse order
		return -a.Compare(b)
	})
	return versions[0], nil
}
