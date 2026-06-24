// produced by schematool 7c318111f3fd9ddfce9b7fdf0bd32865e23d7cca server/internal/scheduler ScheduledJobResource
package v1_1_0

import (
	"github.com/pgEdge/control-plane/server/internal/resource"
)

const ResourceTypeScheduledJob resource.Type = "scheduler.job"

func ScheduledJobResourceIdentifier(id string) resource.Identifier {
	return resource.Identifier{
		ID:   id,
		Type: ResourceTypeScheduledJob,
	}
}

type ScheduledJobResource struct {
	ID        string         `json:"id"`
	CronExpr  string         `json:"cron_expr"`
	Workflow  string         `json:"workflow"`
	Args      map[string]any `json:"args"`
	DependsOn []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"depends_on,omitempty"`
}
