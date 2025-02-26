package database

import "context"

type Orchestrator interface {
	PopulateInstanceSpec(ctx context.Context, spec *InstanceSpec) error
}
