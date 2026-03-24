package swarm

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/rs/zerolog"

	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/zfs"
)

// newDockerHostRunner creates a HostRunner that executes arbitrary commands on
// the host by running them inside a temporary privileged container with
// nsenter. This is needed when the control plane runs in a minimal (distroless)
// container that cannot execute host binaries directly.
func newDockerHostRunner(dockerClient *docker.Docker, image string, logger zerolog.Logger) zfs.HostRunner {
	logger = logger.With().Str("component", "host_runner").Logger()

	return func(name string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		// Use nsenter to enter the host's mount namespace (PID 1) and run the command.
		cmd := append([]string{"nsenter", "--target", "1", "--mount", "--", name}, args...)

		containerID, err := dockerClient.ContainerRun(ctx, docker.ContainerRunOptions{
			Config: &container.Config{
				Image: image,
				Cmd:   cmd,
				Tty:   true, // Avoid Docker log multiplexing headers.
			},
			Host: &container.HostConfig{
				PidMode:    "host",
				Privileged: true,
			},
		})
		if err != nil {
			return "", fmt.Errorf("%s %s: failed to start runner container: %w", name, strings.Join(args, " "), err)
		}
		defer func() {
			rmCtx, rmCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer rmCancel()
			if err := dockerClient.ContainerRemove(rmCtx, containerID, container.RemoveOptions{Force: true}); err != nil {
				logger.Warn().Err(err).Str("container_id", containerID).Msg("failed to remove runner container")
			}
		}()

		waitErr := dockerClient.ContainerWait(ctx, containerID, container.WaitConditionNotRunning, 2*time.Minute)

		var buf bytes.Buffer
		_ = dockerClient.ContainerLogs(ctx, &buf, containerID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
		})
		output := strings.TrimSpace(buf.String())

		if waitErr != nil {
			return "", fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), output, waitErr)
		}
		return output, nil
	}
}
