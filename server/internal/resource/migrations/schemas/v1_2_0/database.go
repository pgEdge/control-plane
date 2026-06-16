// produced by schematool 20b82249f8734cd7aa4ed88b2a0e60a68c7bf058 server/internal/database NodeResource
package v1_2_0

import (
	"github.com/pgEdge/control-plane/server/internal/resource"
)

const ResourceTypeNode resource.Type = "database.node"

func NodeResourceIdentifier(nodeName string) resource.Identifier {
	return resource.Identifier{
		ID:   nodeName,
		Type: ResourceTypeNode,
	}
}

type NodeResource struct {
	Name              string   `json:"name"`
	InstanceIDs       []string `json:"instance_ids"`
	PrimaryInstanceID string   `json:"primary_instance_id"`
}
