package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"time"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pgEdge/control-plane/server/internal/common"
	"github.com/samber/do"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"
)

var ErrNotFound = errors.New("not found error")
var ErrProcessNotFound = errors.New("matching process not found")

var _ common.HealthCheckable = (*Docker)(nil)
var _ do.Shutdownable = (*Docker)(nil)

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

func (d *Docker) Exec(ctx context.Context, w io.Writer, containerID string, command []string) error {
	execIDResp, err := d.client.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Cmd:          command,
	})
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", errTranslate(err))
	}
	resp, err := d.client.ContainerExecAttach(ctx, execIDResp.ID, container.ExecAttachOptions{
		Detach: false,
		Tty:    true,
	})
	if err != nil {
		return fmt.Errorf("failed to attach to exec: %w", err)
	}

	defer resp.Close()
	if _, err := io.Copy(w, resp.Reader); err != nil {
		return fmt.Errorf("failed to copy exec output: %w", err)
	}

	inspResp, err := d.client.ContainerExecInspect(ctx, execIDResp.ID)
	if err != nil {
		return fmt.Errorf("failed to inspect exec: %w", err)
	}

	if inspResp.ExitCode != 0 {
		err = fmt.Errorf("command failed with exit code %d", inspResp.ExitCode)
	}
	return err
}

type ContainerRunOptions struct {
	Config   *container.Config
	Host     *container.HostConfig
	Net      *network.NetworkingConfig
	Platform *v1.Platform
	Name     string
}

func (d *Docker) ContainerRun(ctx context.Context, opts ContainerRunOptions) (string, error) {
	resp, err := d.client.ContainerCreate(ctx, opts.Config, opts.Host, opts.Net, opts.Platform, opts.Name)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}
	if err := d.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}
	return resp.ID, nil
}

func (d *Docker) ContainerLogs(ctx context.Context, w io.Writer, containerID string, opts container.LogsOptions) error {
	resp, err := d.client.ContainerLogs(ctx, containerID, opts)
	if err != nil {
		return errTranslate(fmt.Errorf("failed to get container logs: %w", err))
	}
	defer resp.Close()

	if _, err := io.Copy(w, resp); err != nil {
		return fmt.Errorf("failed to copy logs: %w", err)
	}
	return nil
}

func (d *Docker) ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	respC, errC := d.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case resp := <-respC:
		if resp.Error != nil {
			return fmt.Errorf("failed to wait for container: %w", errTranslate(errors.New(resp.Error.Message)))
		}
		if resp.StatusCode != 0 {
			return fmt.Errorf("container exited with status code %d", resp.StatusCode)
		}
		return nil
	case err := <-errC:
		if err != nil {
			return fmt.Errorf("failed to wait for container: %w", errTranslate(err))
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func (d *Docker) ContainerRemove(ctx context.Context, containerID string, opts container.RemoveOptions) error {
	err := d.client.ContainerRemove(ctx, containerID, opts)
	if err != nil {
		return fmt.Errorf("failed to remove container: %w", errTranslate(err))
	}
	return nil
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

func (d *Docker) ServiceInspect(ctx context.Context, serviceID string) (swarm.Service, error) {
	service, _, err := d.client.ServiceInspectWithRaw(ctx, serviceID, types.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, fmt.Errorf("failed to inspect service: %w", errTranslate(err))
	}
	return service, nil
}

type ServiceDeployOptions struct {
	Spec        swarm.ServiceSpec
	Wait        bool
	WaitTimeout time.Duration
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

func (d *Docker) ServiceList(ctx context.Context, opts types.ServiceListOptions) ([]swarm.Service, error) {
	services, err := d.client.ServiceList(ctx, opts)
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

type ServiceScaleOptions struct {
	ServiceID   string
	Scale       uint64
	Wait        bool
	WaitTimeout time.Duration
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

func (d *Docker) ServiceRemove(ctx context.Context, serviceID string) error {
	err := d.client.ServiceRemove(ctx, serviceID)
	if client.IsErrNotFound(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to remove service: %w", errTranslate(err))
	}
	return nil
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

func (d *Docker) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	resp, err := d.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return types.ContainerJSON{}, fmt.Errorf("failed to inspect container: %w", errTranslate(err))
	}
	return resp, nil
}

func (d *Docker) GetContainerByLabels(ctx context.Context, labels map[string]string) (types.Container, error) {
	var f []filters.KeyValuePair
	for key, value := range labels {
		f = append(f, filters.Arg("label", key+"="+value))
	}
	matches, err := d.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(f...),
	})
	if err != nil {
		return types.Container{}, fmt.Errorf("failed to find matching containers: %w", err)
	}
	if len(matches) == 0 {
		return types.Container{}, fmt.Errorf("%w: no matching containers found", ErrNotFound)
	}
	return matches[0], nil
}

func (d *Docker) Shutdown() error {
	if err := d.client.Close(); err != nil {
		return fmt.Errorf("failed to close docker client: %w", err)
	}
	return nil
}

type NetworkInfo struct {
	Name    string
	ID      string
	Subnet  netip.Prefix
	Gateway netip.Addr
}

func ExtractNetworkInfo(info network.Inspect) (*NetworkInfo, error) {
	if len(info.IPAM.Config) < 1 {
		return nil, errors.New("network has no IPAM configuration")
	}
	subnet, err := netip.ParsePrefix(info.IPAM.Config[0].Subnet)
	if err != nil {
		return nil, fmt.Errorf("failed to parse subnet: %w", err)
	}
	gateway, err := netip.ParseAddr(info.IPAM.Config[0].Gateway)
	if err != nil {
		return nil, fmt.Errorf("failed to parse gateway: %w", err)
	}
	return &NetworkInfo{
		Name:    info.Name,
		ID:      info.ID,
		Subnet:  subnet,
		Gateway: gateway,
	}, nil
}

// The docker errors are annoying to check further up in the stack since they
// rely on type checks. Wrapping them in our own errors makes it easier for
// callers to explicitly handle specific errors.
func errTranslate(err error) error {
	if client.IsErrNotFound(err) {
		return fmt.Errorf("%w: %s", ErrNotFound, err.Error())
	}
	return err
}
