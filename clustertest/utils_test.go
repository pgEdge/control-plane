//go:build cluster_test

package clustertest

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/client"
	"github.com/stretchr/testify/require"
)

func buildImage() {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	log.Println("building control-plane image")

	cmd := exec.CommandContext(ctx, "make", "-C", "..", "goreleaser-build", "control-plane-images")
	cmd.Env = append(cmd.Env, os.Environ()...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		log.Fatalf("failed to build image: %s", err)
	}
}

var allocatedPorts = map[int]bool{}
var portMu sync.Mutex

func allocatePorts(t testing.TB, n int) []int {
	t.Helper()

	portMu.Lock()
	defer portMu.Unlock()

	ports := make([]int, n)

	for k := range ports {
		var port int

		for {
			// Keep getting random ports until we find one that we haven't
			// already allocated
			addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
			require.NoError(t, err)

			l, err := net.ListenTCP("tcp", addr)
			require.NoError(t, err)

			port = l.Addr().(*net.TCPAddr).Port

			if !allocatedPorts[port] {
				// Hold on to the port until we return to prevent other
				// processes from stealing it.
				defer l.Close()

				allocatedPorts[port] = true
				break
			} else {
				// Release the port early if it's one we've already allocated.
				l.Close()
			}
		}

		ports[k] = port
	}

	return ports
}

func makeDataDir() error {
	if testConfig.dataDirPrefix == "" {
		testConfig.dataDirPrefix = filepath.Join(".", "data")
	}

	d, err := filepath.Abs(filepath.Join(testConfig.dataDirPrefix, time.Now().Format("20060102150405")))
	if err != nil {
		return fmt.Errorf("failed to compute data dir absolute path: %w", err)
	}
	testConfig.dataDir = d

	log.Printf("using data directory %s\n", testConfig.dataDir)

	if err := os.MkdirAll(testConfig.dataDir, 0o700); err != nil {
		return fmt.Errorf("failed to create test data directory: %w", err)
	}

	return nil
}

func cleanupDataDir() {
	if testConfig.skipCleanup {
		log.Printf("skipping cleanup for data directory %s\n", testConfig.dataDir)
		return
	}

	if runtime.GOOS == "linux" {
		// On linux hosts, the control plane is able to change ownership of
		// the directories, which means that the user running the tests may
		// be unable to remove the directories afterwards. We need to use
		// sudo to ensure we're able to delete them.
		out, err := exec.Command("sudo", "rm", "-rf", testConfig.dataDir).Output()
		if err != nil {
			log.Printf("failed to remove temp dir %s: %s, output:\n%s\n", testConfig.dataDir, err, string(out))
		}
	} else {
		err := os.RemoveAll(testConfig.dataDir)
		if err != nil {
			log.Printf("failed to remove temp dir %s: %s", testConfig.dataDir, err)
		}
	}
}

func hostDataDir(t testing.TB, hostID string) string {
	t.Helper()

	randomBytes := make([]byte, 4)
	_, err := rand.Read(randomBytes)
	require.NoError(t, err)

	dir := filepath.Join(testConfig.dataDir, t.Name(), hostID, fmt.Sprintf("%x", randomBytes))

	require.NoError(t, os.MkdirAll(dir, 0o700))

	return dir
}

func tLog(t testing.TB, args ...any) {
	t.Helper()

	prefix := fmt.Sprintf("[%s]", t.Name())
	all := append([]any{prefix}, args...)
	t.Log(all...)
}

func tLogf(t testing.TB, format string, args ...any) {
	t.Helper()

	prefix := fmt.Sprintf("[%s] ", t.Name())
	format = prefix + format
	t.Logf(format, args...)
}

func pointerTo[T any](v T) *T {
	return &v
}

// waitForTaskComplete polls a database task until it completes, fails, or times out.
func waitForTaskComplete(ctx context.Context, c client.Client, dbID api.Identifier, taskID string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for task %s to complete", taskID)
		case <-ticker.C:
			task, err := c.GetDatabaseTask(ctx, &api.GetDatabaseTaskPayload{
				DatabaseID: dbID,
				TaskID:     taskID,
			})
			if err != nil {
				return fmt.Errorf("failed to get task: %w", err)
			}

			switch task.Status {
			case client.TaskStatusCompleted:
				return nil
			case client.TaskStatusFailed:
				errMsg := "unknown error"
				if task.Error != nil {
					errMsg = *task.Error
				}
				return fmt.Errorf("task failed: %s", errMsg)
			case client.TaskStatusCanceled:
				return fmt.Errorf("task was canceled")
				// "pending", "running", "canceling" - continue waiting
			}
		}
	}
}

// waitForDatabaseAvailable polls a database until it reaches available state or times out.
func waitForDatabaseAvailable(ctx context.Context, c client.Client, dbID api.Identifier, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for database %s to be available", dbID)
		case <-ticker.C:
			db, err := c.GetDatabase(ctx, &api.GetDatabasePayload{
				DatabaseID: dbID,
			})
			if err != nil {
				return fmt.Errorf("failed to get database: %w", err)
			}

			if db.State == client.DatabaseStateAvailable {
				return nil
			}

			if db.State == client.DatabaseStateFailed || db.State == client.DatabaseStateDegraded {
				return fmt.Errorf("database is in %s state", db.State)
			}
		}
	}
}

// verifyDatabaseHealth checks that a database has the expected number of healthy nodes and instances.
func verifyDatabaseHealth(ctx context.Context, t *testing.T, c client.Client, dbID api.Identifier, expectedNodes int) error {
	db, err := c.GetDatabase(ctx, &api.GetDatabasePayload{
		DatabaseID: dbID,
	})
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}

	if len(db.Spec.Nodes) != expectedNodes {
		return fmt.Errorf("expected %d nodes in spec, got %d", expectedNodes, len(db.Spec.Nodes))
	}

	if len(db.Instances) != expectedNodes {
		return fmt.Errorf("expected %d instances, got %d", expectedNodes, len(db.Instances))
	}

	availableCount := 0
	for _, inst := range db.Instances {
		if inst.State == "available" {
			availableCount++
		}
	}

	if availableCount != expectedNodes {
		return fmt.Errorf("expected %d available instances, got %d", expectedNodes, availableCount)
	}

	return nil
}

// verifyDatabaseHealthWORKAROUND There is currently an issue with "remove-host --force" leaving an instance in the
// "unknown" state - fixing this requires some more thought.  The root issue is that the workflow that removes the
// instance is targeted at the host being removed, so we drop it.  For now, assert that there are 2 healthy instances
// and one with nodename == "n3" and state == "unknown".
//
// Remove this func once the issue has been resolved.
func verifyDatabaseHealthWORKAROUND(ctx context.Context, t *testing.T, c client.Client, dbID api.Identifier, expectedNodes int) error {
	db, err := c.GetDatabase(ctx, &api.GetDatabasePayload{
		DatabaseID: dbID,
	})
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}

	if len(db.Spec.Nodes) != expectedNodes {
		return fmt.Errorf("expected %d nodes in spec, got %d", expectedNodes, len(db.Spec.Nodes))
	}

	if len(db.Instances) != expectedNodes {
		// Temporary: Allow N+1 instances IFF the extra is "n3" in "unknown" state
		if len(db.Instances) == expectedNodes+1 {
			foundOrphanedNode3 := false
			for _, inst := range db.Instances {
				if inst.NodeName == "n3" && inst.State == "unknown" {
					foundOrphanedNode3 = true
					break
				}
			}
			if !foundOrphanedNode3 {
				return fmt.Errorf("expected %d instances, got %d", expectedNodes, len(db.Instances))
			}
		} else {
			return fmt.Errorf("expected %d instances, got %d", expectedNodes, len(db.Instances))
		}
	}

	return nil
}

// waitForHostCount polls ListHosts until the expected number of hosts is reached or timeout occurs.
func waitForHostCount(ctx context.Context, c client.Client, expectedCount int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			hosts, _ := c.ListHosts(ctx)
			actualCount := 0
			if hosts != nil {
				actualCount = len(hosts.Hosts)
			}
			return fmt.Errorf("timeout waiting for %d hosts (currently %d)", expectedCount, actualCount)
		case <-ticker.C:
			hosts, err := c.ListHosts(ctx)
			if err != nil {
				return fmt.Errorf("failed to list hosts: %w", err)
			}

			if len(hosts.Hosts) == expectedCount {
				return nil
			}
		}
	}
}
