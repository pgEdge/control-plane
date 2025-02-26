package host

import "context"

type Orchestrator interface {
	PopulateHost(ctx context.Context, h *Host) error
	PopulateHostStatus(ctx context.Context, h *HostStatus) error
}
