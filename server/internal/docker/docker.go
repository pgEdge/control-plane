package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pgEdge/control-plane/server/internal/common"
	"github.com/samber/do"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
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
	logger zerolog.Logger
}

func NewDocker(logger zerolog.Logger) (*Docker, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	return &Docker{
		client: cli,
		logger: logger.With().Str("component", "docker").Logger(),
	}, nil
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
	if opts.Config != nil {
		if opts.Config.Image == "" {
			return "", errors.New("image must be specified in container config")
		}
		if err := d.ensureDockerImage(ctx, opts.Config.Image); err != nil {
			return "", fmt.Errorf("failed to ensure docker image %q: %w", opts.Config.Image, err)
		}
	}
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

func (d *Docker) ContainerStop(ctx context.Context, containerID string, timeoutSeconds *int) error {
	err := d.client.ContainerStop(ctx, containerID, container.StopOptions{Timeout: timeoutSeconds})
	if err != nil {
		return fmt.Errorf("failed to stop container: %w", errTranslate(err))
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

func (d *Docker) ServiceInspectByLabels(ctx context.Context, labels map[string]string) (swarm.Service, error) {
	var f []filters.KeyValuePair
	for key, value := range labels {
		f = append(f, filters.Arg("label", key+"="+value))
	}
	matches, err := d.ServiceList(ctx, types.ServiceListOptions{
		Filters: filters.NewArgs(f...),
	})
	if err != nil {
		return swarm.Service{}, fmt.Errorf("failed to find matching services: %w", err)
	}
	if len(matches) == 0 {
		return swarm.Service{}, fmt.Errorf("%w: no matching services found", ErrNotFound)
	}
	return matches[0], nil
}

type ServiceDeployResult struct {
	ServiceID string
	Previous  swarm.Version
}

func (d *Docker) ServiceDeploy(ctx context.Context, spec swarm.ServiceSpec) (ServiceDeployResult, error) {
	existing, _, err := d.client.ServiceInspectWithRaw(ctx, spec.Name, types.ServiceInspectOptions{})
	switch {
	case client.IsErrNotFound(err):
		// service does not exist, create it
		resp, err := d.client.ServiceCreate(ctx, spec, types.ServiceCreateOptions{})
		if err != nil {
			return ServiceDeployResult{}, fmt.Errorf("failed to create service: %w", err)
		}
		return ServiceDeployResult{ServiceID: resp.ID}, nil
	case err == nil:
		// service does exist, update it
		_, err := d.client.ServiceUpdate(ctx, existing.ID, existing.Version, spec, types.ServiceUpdateOptions{})
		if err != nil {
			return ServiceDeployResult{}, fmt.Errorf("failed to update service: %w", errTranslate(err))
		}
		return ServiceDeployResult{
			ServiceID: existing.ID,
			Previous:  existing.Version,
		}, nil
	default:
		return ServiceDeployResult{}, fmt.Errorf("failed to check for existing service: %w", err)
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

	return d.WaitForService(ctx, opts.ServiceID, opts.WaitTimeout, service.Version)
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

func (d *Docker) TaskList(ctx context.Context, filter filters.Args) ([]swarm.Task, error) {
	tasks, err := d.client.TaskList(ctx, types.TaskListOptions{
		Filters: filter,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", errTranslate(err))
	}
	return tasks, nil
}

// WaitForService waits until the given service achieves the desired state and
// number of tasks. The Swarm API can return stale data before the updated spec
// has propagated to all manager nodes, so the optional 'previous swarm.Version'
// parameter can be used to detect stale reads.
func (d *Docker) WaitForService(ctx context.Context, serviceID string, timeout time.Duration, previous swarm.Version) error {
	if timeout == 0 {
		timeout = time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			service, _, err := d.client.ServiceInspectWithRaw(ctx, serviceID, types.ServiceInspectOptions{})
			if err != nil {
				return fmt.Errorf("failed to inspect service: %w", errTranslate(err))
			}
			if service.Spec.Mode.Replicated == nil {
				return fmt.Errorf("WaitForService is only usable for replicated services")
			}
			if service.UpdateStatus != nil && service.UpdateStatus.State != swarm.UpdateStateCompleted {
				d.logger.Debug().
					Str("service_id", serviceID).
					Str("update_status", string(service.UpdateStatus.State)).
					Msg("service update in progress, waiting")
				continue
			}
			if service.Version.Index == previous.Index {
				// The old service version was returned
				d.logger.Debug().
					Str("service_id", serviceID).
					Uint64("version_index", service.Version.Index).
					Msg("old service version returned, waiting")
				continue
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
				return fmt.Errorf("failed to list tasks for service: %w", errTranslate(err))
			}
			var running, stopping, failed, preparing, pending uint64
			var lastFailureMsg string
			var taskStates []string
			for _, t := range tasks {
				taskStates = append(taskStates, string(t.Status.State))
				switch t.Status.State {
				case swarm.TaskStateRunning:
					if t.DesiredState == swarm.TaskStateRunning {
						running++
					} else {
						stopping++
					}
				case swarm.TaskStateFailed, swarm.TaskStateRejected:
					failed++
					// Capture the error message from the most recent failed task
					if t.Status.Err != "" {
						lastFailureMsg = t.Status.Err
					} else if t.Status.Message != "" {
						lastFailureMsg = t.Status.Message
					}
				case swarm.TaskStatePreparing:
					preparing++
				case swarm.TaskStatePending, swarm.TaskStateAssigned, swarm.TaskStateAccepted:
					pending++
				}
			}

			d.logger.Debug().
				Str("service_id", serviceID).
				Uint64("desired", desired).
				Uint64("running", running).
				Uint64("stopping", stopping).
				Uint64("failed", failed).
				Uint64("preparing", preparing).
				Uint64("pending", pending).
				Int("total_tasks", len(tasks)).
				Strs("task_states", taskStates).
				Msg("checking service task status")

			// If we have failed tasks and no running or transitional tasks, the service won't start
			if failed > 0 && running == 0 && preparing == 0 && pending == 0 {
				if lastFailureMsg != "" {
					return fmt.Errorf("service tasks failed: %s", lastFailureMsg)
				}
				return fmt.Errorf("service has %d failed task(s), expected %d running", failed, desired)
			}

			if running == desired && stopping == 0 {
				d.logger.Info().
					Str("service_id", serviceID).
					Uint64("running_tasks", running).
					Msg("service tasks ready")
				return nil
			}
		}
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

func (d *Docker) ensureDockerImage(ctx context.Context, img string) error {
	// Pull the image
	reader, err := d.client.ImagePull(ctx, img, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %q: %w", img, err)
	}
	defer reader.Close()

	// Read the output from the pull operation
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("failed to read image pull output: %w", err)
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

// This is an unexported error type, so we have limited options to test for
// it. This error message has been stable for 9 years, so it's likely safe
// to rely on.
// https://github.com/moby/moby/blob/cab4ac834e8bf36aa38a2ca49599773df6e6805a/volume/mounts/validate.go#L16
const bindMountErrPrefix = `invalid mount config for type "bind":`

// ExtractBindError extracts the bind error message from the given error if it
// is a bind error. Otherwise, returns an empty string.
func ExtractBindMountErrorMsg(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	idx := strings.Index(msg, bindMountErrPrefix)
	if idx < 0 {
		return ""
	}

	return strings.TrimPrefix(msg[idx:], bindMountErrPrefix)
}

// ExtractPortErrorMsg extracts the port bind error message from the given error
// if it is a port bind or allocation error. Otherwise, returns an empty string.
func ExtractPortErrorMsg(err error) string {
	if err == nil {
		return ""
	}
	if msg := extractPortBindErrorMsg(err); msg != "" {
		return msg
	}
	if msg := extractPortAlreadyAllocatedErrorMsg(err); msg != "" {
		return msg
	}
	return ""
}

// More internal error messages:
// https://github.com/moby/moby/blob/cab4ac834e8bf36aa38a2ca49599773df6e6805a/libnetwork/drivers/bridge/port_mapping_linux.go#L622-L627
// This one is less stable, so we'll do our best. In the worst case, we return a
// 500 with a longer error message, which will still be helpful to the user.
const portBindErrPrefix = `failed to bind`

func extractPortBindErrorMsg(err error) string {
	msg := err.Error()
	idx := strings.Index(msg, portBindErrPrefix)
	if idx < 0 {
		return ""
	}

	return msg[idx:]
}

// https://github.com/moby/moby/blob/9b4f68d64cde951e5b985a0c589f16f1416d3968/libnetwork/portallocator/portallocator.go#L33
// This message has been stable for 10 years.
var portAlreadyAllocatedPattern = regexp.MustCompile(`Bind for .* failed: port is already allocated`)

func extractPortAlreadyAllocatedErrorMsg(err error) string {
	return portAlreadyAllocatedPattern.FindString(err.Error())
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

func BuildMount(source, target string, readOnly bool) mount.Mount {
	return mount.Mount{
		Type:     mount.TypeBind,
		Source:   source,
		Target:   target,
		ReadOnly: readOnly,
	}
}

// https://github.com/docker/compose/issues/3012
// https://forums.docker.com/t/getting-no-such-network-errors-starting-a-stack-in-a-swarm/41202
// https://forums.docker.com/t/docker-network-not-found-by-docker-compose/117171
// https://forums.docker.com/t/cant-attach-a-standalone-container-to-a-multi-host-overlay-network/117933/10

func ExtractNetworkErrorMsg(err error) string {
	if err == nil {
		return ""
	}

	msg := err.Error()

	switch {
	case strings.Contains(msg, "endpoint with name"):
		return "Network endpoint conflict: " + msg
	case strings.Contains(msg, "network-scoped alias"):
		return "Invalid network-scoped alias: " + msg
	case strings.Contains(msg, "not attachable"):
		return "Swarm network is not attachable: " + msg
	case strings.Contains(msg, "No such network"):
		return "Network not found: " + msg
	case strings.Contains(msg, "could not be found"):
		return "Network configuration error: " + msg
	case strings.Contains(msg, "could not attach"):
		return "Container could not attach to the network: " + msg
	default:
		return ""
	}
}
