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

	"github.com/stretchr/testify/require"
)

func buildImage() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
