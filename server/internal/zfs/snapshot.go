package zfs

import (
	"context"
	"fmt"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

var _ resource.Resource = (*Snapshot)(nil)

// Snapshot is a ZFS resource that creates a snapshot of a source instance's
// dataset to serve as the basis for a clone.
type Snapshot struct {
	SourceInstanceID string `json:"source_instance_id"`
	CloneInstanceID  string `json:"clone_instance_id"`
	HostID           string `json:"host_id"`
	Pool             string `json:"pool"`

	// Run is an injectable CommandRunner for testability. If nil,
	// DefaultCommandRunner is used.
	Run CommandRunner `json:"-"`
}

func (s *Snapshot) runner() CommandRunner {
	if s.Run != nil {
		return s.Run
	}
	return DefaultCommandRunner
}

func (s *Snapshot) ResourceVersion() string {
	return "1"
}

func (s *Snapshot) DiffIgnore() []string {
	return nil
}

func (s *Snapshot) Executor() resource.Executor {
	return resource.HostExecutor(s.HostID)
}

func (s *Snapshot) Identifier() resource.Identifier {
	return resource.Identifier{Type: ResourceTypeSnapshot, ID: s.CloneInstanceID}
}

func (s *Snapshot) Dependencies() []resource.Identifier {
	return nil
}

func (s *Snapshot) TypeDependencies() []resource.Type {
	return nil
}

func (s *Snapshot) Refresh(_ context.Context, _ *resource.Context) error {
	name := SnapshotName(s.Pool, s.SourceInstanceID, s.CloneInstanceID)
	exists, err := datasetExists(s.runner(), name)
	if err != nil {
		return fmt.Errorf("failed to check snapshot %q: %w", name, err)
	}
	if !exists {
		return resource.ErrNotFound
	}
	return nil
}

func (s *Snapshot) Create(_ context.Context, _ *resource.Context) error {
	name := SnapshotName(s.Pool, s.SourceInstanceID, s.CloneInstanceID)
	_, err := s.runner()("snapshot", name)
	if err != nil {
		return fmt.Errorf("failed to create snapshot %q: %w", name, err)
	}
	return nil
}

func (s *Snapshot) Update(ctx context.Context, rc *resource.Context) error {
	return s.Create(ctx, rc)
}

func (s *Snapshot) Delete(_ context.Context, _ *resource.Context) error {
	name := SnapshotName(s.Pool, s.SourceInstanceID, s.CloneInstanceID)
	_, err := s.runner()("destroy", name)
	if err != nil {
		return fmt.Errorf("failed to destroy snapshot %q: %w", name, err)
	}
	return nil
}
