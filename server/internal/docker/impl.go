package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/pgEdge/control-plane/server/internal/common"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"
)

var _ common.HealthCheckable = (*Docker)(nil)

// var _ Docker = (*docker)(nil)

type Docker struct {
	client *client.Client
}

func NewDocker() (*Docker, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	return &Docker{client: cli}, nil
}

func (d *Docker) Exec(ctx context.Context, containerID string, command []string) (string, error) {
	execIDResp, err := d.client.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Cmd:          command,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create exec: %w", errTranslate(err))
	}
	resp, err := d.client.ContainerExecAttach(ctx, execIDResp.ID, container.ExecAttachOptions{
		Detach: false,
		Tty:    true,
	})
	if err != nil {
		return "", fmt.Errorf("failed to attach to exec: %w", err)
	}

	defer resp.Close()
	output, err := io.ReadAll(resp.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to read exec output: %w", err)
	}

	inspResp, err := d.client.ContainerExecInspect(ctx, execIDResp.ID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect exec: %w", err)
	}

	if inspResp.ExitCode != 0 {
		err = fmt.Errorf("command failed with exit code %d: %s", inspResp.ExitCode, output)
	}
	return string(output), err
}

func (d *Docker) Info(ctx context.Context) (system.Info, error) {
	info, err := d.client.Info(ctx)
	if err != nil {
		return system.Info{}, fmt.Errorf("failed to get docker info: %w", err)
	}
	return info, nil
}

func (d *Docker) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (string, error) {
	resp, err := d.client.NetworkCreate(ctx, name, options)
	if err != nil {
		return "", fmt.Errorf("failed to create network: %w", err)
	}
	return resp.ID, nil
}

func (d *Docker) NetworkInspect(ctx context.Context, name string, options network.InspectOptions) (network.Inspect, error) {
	resp, err := d.client.NetworkInspect(ctx, name, options)
	if err != nil {
		return network.Inspect{}, fmt.Errorf("failed to inspect network: %w", errTranslate(err))
	}
	return resp, nil
}

func (d *Docker) NetworkRemove(ctx context.Context, networkID string) error {
	err := d.client.NetworkRemove(ctx, networkID)
	if err != nil {
		return fmt.Errorf("failed to remove network: %w", errTranslate(err))
	}
	return nil
}

func (d *Docker) NodeList(ctx context.Context) ([]swarm.Node, error) {
	nodes, err := d.client.NodeList(ctx, types.NodeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	return nodes, nil
}

func (d *Docker) ServiceDeploy(ctx context.Context, spec swarm.ServiceSpec) (string, error) {
	existing, _, err := d.client.ServiceInspectWithRaw(ctx, spec.Name, types.ServiceInspectOptions{})
	if client.IsErrNotFound(err) {
		// service does not exist, create it
		resp, err := d.client.ServiceCreate(ctx, spec, types.ServiceCreateOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to create service: %w", err)
		}
		return resp.ID, nil
	} else if err == nil {
		// service exists, update it
		_, err := d.client.ServiceUpdate(ctx, existing.ID, existing.Version, spec, types.ServiceUpdateOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to update service: %w", errTranslate(err))
		}
		return existing.ID, nil
	} else {
		return "", fmt.Errorf("failed to check for existing service: %w", err)
	}
}

func (d *Docker) ServiceList(ctx context.Context, opts ServiceListOptions) ([]swarm.Service, error) {
	services, err := d.client.ServiceList(ctx, types.ServiceListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}
	return services, nil
}

func (d *Docker) ServiceRestart(ctx context.Context, serviceID string, targetScale uint64, scaleTimeout time.Duration) error {
	if err := d.ServiceScale(ctx, ServiceScaleOptions{
		ServiceID:   serviceID,
		Scale:       0,
		Wait:        true,
		WaitTimeout: scaleTimeout,
	}); err != nil {
		return fmt.Errorf("failed to scale down service %q: %w", serviceID, err)
	}

	// Brief sleep to avoid issues from rapid restarts.
	time.Sleep(time.Second * 5)

	if err := d.ServiceScale(ctx, ServiceScaleOptions{
		ServiceID:   serviceID,
		Scale:       targetScale,
		Wait:        true,
		WaitTimeout: scaleTimeout,
	}); err != nil {
		return fmt.Errorf("failed to scale up service %q: %w", serviceID, err)
	}

	return nil
}

func (d *Docker) ServiceScale(ctx context.Context, opts ServiceScaleOptions) error {
	// timeout := opts.WaitTimeout
	// if timeout == 0 {
	// 	timeout = time.Minute
	// }
	// ctx, cancel := context.WithTimeout(ctx, timeout)
	// defer cancel()

	// adapted from https://github.com/docker/cli/blob/master/cli/command/service/scale.go
	service, _, err := d.client.ServiceInspectWithRaw(ctx, opts.ServiceID, types.ServiceInspectOptions{})
	if err != nil {
		return fmt.Errorf("failed to inspect service: %w", errTranslate(err))
	}

	serviceMode := &service.Spec.Mode
	switch {
	case serviceMode.Replicated != nil:
		serviceMode.Replicated.Replicas = &opts.Scale
	case serviceMode.ReplicatedJob != nil:
		serviceMode.ReplicatedJob.TotalCompletions = &opts.Scale
	default:
		return errors.New("scale can only be used with replicated or replicated-job mode services")
	}

	_, err = d.client.ServiceUpdate(ctx, service.ID, service.Version, service.Spec, types.ServiceUpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update service: %w", errTranslate(err))
	}

	if !opts.Wait {
		return nil
	}

	return d.WaitForService(ctx, opts.ServiceID, opts.WaitTimeout)
}

func (d *Docker) TasksByServiceID(ctx context.Context) (map[string][]swarm.Task, error) {
	tasks, err := d.client.TaskList(ctx, types.TaskListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", errTranslate(err))
	}
	tasksByServiceID := map[string][]swarm.Task{}
	for _, t := range tasks {
		tasksByServiceID[t.ServiceID] = append(tasksByServiceID[t.ServiceID], t)
	}

	return tasksByServiceID, nil
}

func (d *Docker) WaitForService(ctx context.Context, serviceID string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	errChan := make(chan error)
	go func() {
		for {
			service, _, err := d.client.ServiceInspectWithRaw(ctx, serviceID, types.ServiceInspectOptions{})
			if err != nil {
				errChan <- fmt.Errorf("failed to inspect service: %w", errTranslate(err))
			}
			if service.Spec.Mode.Replicated == nil {
				errChan <- fmt.Errorf("WaitForServiceScale is only usable for replicated services: %w", err)
			}
			var desired uint64
			if r := service.Spec.Mode.Replicated.Replicas; r != nil {
				desired = *r
			}
			tasks, err := d.client.TaskList(ctx, types.TaskListOptions{
				Filters: filters.NewArgs(filters.KeyValuePair{
					Key:   "service",
					Value: service.ID,
				}),
			})
			if err != nil {
				errChan <- fmt.Errorf("failed to list tasks for service: %w", errTranslate(err))
			}
			var running uint64
			for _, t := range tasks {
				if t.Status.State == swarm.TaskStateRunning {
					running++
				}
			}
			if running == desired {
				errChan <- nil
			}
			time.Sleep(time.Second)
		}
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return fmt.Errorf("timed out waiting for service %q to scale", serviceID)
	}
}

func (d *Docker) HealthCheck() common.ComponentStatus {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	info, err := d.Info(ctx)
	if err != nil {
		return common.ComponentStatus{
			Name:    "docker",
			Healthy: false,
			Error:   err.Error(),
		}
	}

	return common.ComponentStatus{
		Name:    "docker",
		Healthy: info.Swarm.LocalNodeState == swarm.LocalNodeStateActive,
		Details: map[string]any{
			"containers":              info.Containers,
			"containers_running":      info.ContainersRunning,
			"containers_stopped":      info.ContainersStopped,
			"containers_paused":       info.ContainersPaused,
			"swarm.local_node_state":  info.Swarm.LocalNodeState,
			"swarm.control_available": info.Swarm.ControlAvailable,
			"swarm.error":             info.Swarm.Error,
		},
	}
}

func (d *Docker) ContainerList(ctx context.Context, opts container.ListOptions) ([]types.Container, error) {
	containers, err := d.client.ContainerList(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	return containers, nil
}

// The docker errors don't annoying to check further up in the stack since they
// rely on type checks. Wrapping them in our own errors makes it easier for
// callers to explicitly handle specific errors.
func errTranslate(err error) error {
	if client.IsErrNotFound(err) {
		return fmt.Errorf("%w: %s", ErrNotFound, err.Error())
	}
	return err
}
