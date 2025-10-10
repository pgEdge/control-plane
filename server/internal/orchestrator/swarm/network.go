package swarm

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/docker/docker/api/types/network"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/ipam"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
)

var _ resource.Resource = (*Network)(nil)

const ResourceTypeNetwork resource.Type = "swarm.network"

func NetworkResourceIdentifier(name string) resource.Identifier {
	return resource.Identifier{
		ID:   name,
		Type: ResourceTypeNetwork,
	}
}

type Allocator struct {
	Prefix netip.Prefix `json:"prefix"`
	Bits   int          `json:"bits"`
}

type Network struct {
	Scope     string       `json:"scope"`
	Driver    string       `json:"driver"`
	Allocator Allocator    `json:"allocator"`
	Name      string       `json:"name"`
	NetworkID string       `json:"network_id"`
	Subnet    netip.Prefix `json:"subnet"`
	Gateway   netip.Addr   `json:"gateway"`
}

func (n *Network) ResourceVersion() string {
	return "1"
}

func (n *Network) DiffIgnore() []string {
	return []string{
		"/network_id",
		"/subnet",
		"/gateway",
	}
}

func (n *Network) Identifier() resource.Identifier {
	return NetworkResourceIdentifier(n.Name)
}

func (n *Network) Executor() resource.Executor {
	return resource.Executor{
		Type: resource.ExecutorTypeCohort,
	}
}

func (n *Network) Dependencies() []resource.Identifier {
	return nil
}

func (n *Network) Validate() error {
	var errs []error
	if n.Scope == "" {
		errs = append(errs, errors.New("scope: cannot be empty"))
	}
	if n.Driver == "" {
		errs = append(errs, errors.New("driver: cannot be empty"))
	}
	if n.Name == "" {
		errs = append(errs, errors.New("name: cannot be empty"))
	}
	if !n.Allocator.Prefix.IsValid() {
		errs = append(errs, errors.New("allocator.prefix: invalid"))
	}
	if n.Allocator.Bits == 0 {
		errs = append(errs, errors.New("allocator.bits: cannot be 0"))
	}
	return errors.Join(errs...)
}

func (n *Network) Refresh(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}
	resp, err := client.NetworkInspect(ctx, n.Name, network.InspectOptions{
		Scope: n.Scope,
	})
	if errors.Is(err, docker.ErrNotFound) {
		return resource.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("failed to inspect network %q: %w", n.Name, err)
	}
	info, err := docker.ExtractNetworkInfo(resp)
	if err != nil {
		return fmt.Errorf("failed to extract network info: %w", err)
	}

	n.NetworkID = resp.ID
	n.Subnet = info.Subnet
	n.Gateway = info.Gateway

	return nil
}

func (n *Network) Create(ctx context.Context, rc *resource.Context) error {
	ipamSvc, err := do.Invoke[*ipam.Service](rc.Injector)
	if err != nil {
		return err
	}
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	err = n.Refresh(ctx, rc)
	if err == nil {
		return nil
	} else if !errors.Is(err, resource.ErrNotFound) {
		return fmt.Errorf("failed to check for existing network: %w", err)
	}
	// Network does not exist, proceed with creation

	subnet, err := ipamSvc.AllocateSubnet(ctx, n.Allocator.Prefix, n.Allocator.Bits)
	if err != nil {
		return fmt.Errorf("failed to allocate subnet: %w", err)
	}
	gateway := subnet.Addr().Next()
	networkID, err := client.NetworkCreate(ctx, n.Name, network.CreateOptions{
		Scope:  n.Scope,
		Driver: n.Driver,
		IPAM: &network.IPAM{
			Config: []network.IPAMConfig{
				{
					Subnet:  subnet.String(),
					Gateway: gateway.String(),
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}
	n.NetworkID = networkID
	n.Subnet = subnet
	n.Gateway = gateway
	return nil
}

func (n *Network) Update(ctx context.Context, rc *resource.Context) error {
	return n.Create(ctx, rc)
}
func (n *Network) Delete(ctx context.Context, rc *resource.Context) error {
	client, err := do.Invoke[*docker.Docker](rc.Injector)
	if err != nil {
		return err
	}

	// TODO: need to add a deallocate method to the ipam service

	err = client.NetworkRemove(ctx, n.Name)
	if errors.Is(err, docker.ErrNotFound) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to remove network %q: %w", n.Name, err)
	}

	return nil
}
