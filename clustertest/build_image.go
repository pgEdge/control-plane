package clustertest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	buildOnce     sync.Once
	builtImageTag string
	buildErr      error
)

// buildControlPlaneImage builds the control-plane Docker image from source,
// caching the result so it runs only once per test run.
func buildControlPlaneImage(ctx context.Context, t *testing.T) (string, error) {
	t.Helper()

	buildOnce.Do(func() {
		builtImageTag, buildErr = doBuild(ctx, t)
	})

	return builtImageTag, buildErr
}

func doBuild(ctx context.Context, t *testing.T) (string, error) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		return "", fmt.Errorf("failed to resolve repo root: %w", err)
	}

	steps := []struct {
		desc string
		cmd  *exec.Cmd
	}{
		{
			desc: "build control-plane binary",
			cmd:  exec.CommandContext(ctx, "make", "dev-build"),
		},
		{
			desc: "build Docker image",
			cmd:  exec.CommandContext(ctx, "docker", "build", "-t", "control-plane-clustertest:latest", "docker/control-plane-dev"),
		},
	}

	for _, step := range steps {
		t.Logf("Starting: %s...", step.desc)
		step.cmd.Dir = repoRoot
		step.cmd.Stdout = os.Stdout
		step.cmd.Stderr = os.Stderr

		if err := step.cmd.Run(); err != nil {
			return "", fmt.Errorf("%s failed: %w", step.desc, err)
		}
		t.Logf("%s complete", step.desc)
	}

	return "control-plane-clustertest:latest", nil
}
