package zfs

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

const (
	ResourceTypeDataset  resource.Type = "zfs_dataset"
	ResourceTypeSnapshot resource.Type = "zfs_snapshot"
	ResourceTypeClone    resource.Type = "zfs_clone"
	ResourceTypeScrub    resource.Type = "zfs_scrub_replication_state"
	ResourceTypeCleanup  resource.Type = "zfs_spock_cleanup"
)

// CommandRunner abstracts ZFS command execution for testability.
type CommandRunner func(args ...string) (string, error)

// HostRunner abstracts execution of arbitrary commands on the host.
type HostRunner func(name string, args ...string) (string, error)

// DefaultCommandRunner executes zfs commands via os/exec.
var DefaultCommandRunner CommandRunner = func(args ...string) (string, error) {
	cmd := exec.Command("zfs", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("zfs %s: %s: %w", strings.Join(args, " "), string(out), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// DefaultHostRunner executes arbitrary commands on the host via os/exec.
var DefaultHostRunner HostRunner = func(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), string(out), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// hostChown changes ownership of a path using the host runner.
func hostChown(path string, uid, gid int) (string, error) {
	return DefaultHostRunner("chown", fmt.Sprintf("%d:%d", uid, gid), path)
}

func RegisterResourceTypes(registry *resource.Registry) {
	resource.RegisterResourceType[*Dataset](registry, ResourceTypeDataset)
	resource.RegisterResourceType[*Snapshot](registry, ResourceTypeSnapshot)
	resource.RegisterResourceType[*Clone](registry, ResourceTypeClone)
	resource.RegisterResourceType[*ScrubReplicationState](registry, ResourceTypeScrub)
	resource.RegisterResourceType[*SpockCleanup](registry, ResourceTypeCleanup)
}

func DatasetName(pool, instanceID string) string {
	return fmt.Sprintf("%s/instances/%s", pool, instanceID)
}

func SnapshotName(pool, sourceInstanceID, cloneInstanceID string) string {
	return fmt.Sprintf("%s/instances/%s@clone-%s", pool, sourceInstanceID, cloneInstanceID)
}

func DatasetIdentifier(instanceID string) resource.Identifier {
	return resource.Identifier{Type: ResourceTypeDataset, ID: instanceID}
}

func SnapshotIdentifier(cloneInstanceID string) resource.Identifier {
	return resource.Identifier{Type: ResourceTypeSnapshot, ID: cloneInstanceID}
}

func CloneIdentifier(cloneInstanceID string) resource.Identifier {
	return resource.Identifier{Type: ResourceTypeClone, ID: cloneInstanceID}
}

func ScrubIdentifier(cloneInstanceID string) resource.Identifier {
	return resource.Identifier{Type: ResourceTypeScrub, ID: cloneInstanceID}
}

func CleanupIdentifier(cloneInstanceID string) resource.Identifier {
	return resource.Identifier{Type: ResourceTypeCleanup, ID: cloneInstanceID}
}

// datasetExists checks if a ZFS dataset or snapshot exists.
func datasetExists(run CommandRunner, name string) (bool, error) {
	_, err := run("list", "-t", "all", "-H", name)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
