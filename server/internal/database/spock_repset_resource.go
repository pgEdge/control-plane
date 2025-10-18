package database

import (
	"context"
	"encoding/json"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

const ResourceTypeSpockRepsetBackup resource.Type = "database.spock_repset_backup"

func SpockRepsetBackupIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{
		ID:   instanceID,
		Type: ResourceTypeSpockRepsetBackup,
	}
}

type SpockRepsetBackupResource struct {
	InstanceID string          `json:"instance_id"`
	HostID     string          `json:"host_id,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

var _ resource.Resource = (*SpockRepsetBackupResource)(nil)

func (s *SpockRepsetBackupResource) ResourceVersion() string {
	return "1"
}

func (s *SpockRepsetBackupResource) DiffIgnore() []string {
	return nil
}

func (s *SpockRepsetBackupResource) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeHost,
		ID:   s.HostID,
	}
}

func (s *SpockRepsetBackupResource) Identifier() resource.Identifier {
	return SpockRepsetBackupIdentifier(s.InstanceID)
}

func (s *SpockRepsetBackupResource) Dependencies() []resource.Identifier {
	return nil
}

func (s *SpockRepsetBackupResource) Refresh(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (s *SpockRepsetBackupResource) Create(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (s *SpockRepsetBackupResource) Update(ctx context.Context, rc *resource.Context) error {
	return nil
}

func (s *SpockRepsetBackupResource) Delete(ctx context.Context, rc *resource.Context) error {
	return nil
}
