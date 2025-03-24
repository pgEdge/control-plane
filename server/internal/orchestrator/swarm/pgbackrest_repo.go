package swarm

import "github.com/google/uuid"

type PgBackRestRepo struct {
	HostID     uuid.UUID `json:"host_id"`
	InstanceID uuid.UUID `json:"instance_id"`
}
