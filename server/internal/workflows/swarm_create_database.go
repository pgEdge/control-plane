package workflows

import "github.com/cschleiden/go-workflows/workflow"

const SwarmCreateDatabase = "SwarmCreateDatabase"

type SwarmCreateDatabaseInput struct {
}

func NewSwarmCreateDatabase() func(ctx workflow.Context, input *SwarmCreateDatabaseInput) error {
	return func(ctx workflow.Context, input *SwarmCreateDatabaseInput) error {
		// Create filesystem
		// Create service
		return nil
	}
}
