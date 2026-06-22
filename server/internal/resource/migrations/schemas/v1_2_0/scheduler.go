// produced by schematool 4d916a246d1ef9e10e2b697e1aaf3b2bc1c2ea21 server/internal/scheduler ScheduledJobResource
package v1_2_0

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
