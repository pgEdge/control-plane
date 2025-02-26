package docker

import (
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
)

var ErrNotFound = errors.New("not found error")

// type Docker interface {
// 	Exec(ctx context.Context, containerID string, command []string) (string, error)
// 	Info(ctx context.Context) (system.Info, error)
// 	NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (string, error)
// 	NetworkInspect(ctx context.Context, name string, options network.InspectOptions) (network.Inspect, error)
// 	NetworkRemove(ctx context.Context, networkID string) error
// 	NodeList(ctx context.Context) ([]swarm.Node, error)
// 	ServiceDeploy(ctx context.Context, spec swarm.ServiceSpec) (string, error)
// 	ServiceList(ctx context.Context, opts ServiceListOptions) ([]swarm.Service, error)
// 	ServiceRestart(ctx context.Context, serviceID string, targetScale uint64, scaleTimeout time.Duration) error
// 	ServiceScale(ctx context.Context, opts ServiceScaleOptions) error
// 	TasksByServiceID(ctx context.Context) (map[string][]swarm.Task, error)
// 	WaitForService(ctx context.Context, serviceID string, timeout time.Duration) error
// }

type ServiceDeployOptions struct {
	Spec        swarm.ServiceSpec
	Wait        bool
	WaitTimeout time.Duration
}

type ServiceListOptions struct {
	Labels map[string]string
}

type ServiceScaleOptions struct {
	ServiceID   string
	Scale       uint64
	Wait        bool
	WaitTimeout time.Duration
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
