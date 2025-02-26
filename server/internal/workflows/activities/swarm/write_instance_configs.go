package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/docker/docker/api/types/network"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/patroni"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities/general"
)

// const SwarmWriteInstanceConfigs = "SwarmWriteInstanceConfigs"

type WriteInstanceConfigsInput struct {
	Host             *host.Host
	HostPaths        HostPaths              `json:"host_paths"`
	Spec             *database.InstanceSpec `json:"spec"`
	InstanceHostname string                 `json:"instance_hostname"`
	// BridgeNetwork    NetworkInfo            `json:"bridge_network"`
	DatabaseNetwork NetworkInfo `json:"database_network"`
	// MemoryBytes      int64                  `json:"memory_bytes"`
	// CPUs             float64                `json:"cpus"`
	ClusterSize int           `json:"cluster_size"`
	Owner       general.Owner `json:"owner,omitempty"`
	// EtcdUsername string        `json:"etcd_username"`
	// Name  string `json:"string"`
}

func (i *WriteInstanceConfigsInput) Validate() error {
	var errs []error
	if i.Spec == nil {
		errs = append(errs, errors.New("spec: must be provided"))
	}
	// for _, err := range i.BridgeNetwork.Validate() {
	// 	errs = append(errs, fmt.Errorf("bridge_network: %w", err))
	// }
	for _, err := range i.DatabaseNetwork.Validate() {
		errs = append(errs, fmt.Errorf("database_network: %w", err))
	}
	return errors.Join(errs...)
}

type WriteInstanceConfigsOutput struct{}

func (a *Activities) ExecuteWriteInstanceConfigs(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *WriteInstanceConfigsInput,
) workflow.Future[*WriteInstanceConfigsOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(hostID.String()),
	}
	return workflow.ExecuteActivity[*WriteInstanceConfigsOutput](ctx, options, a.WriteInstanceConfigs, input)
}

func (a *Activities) WriteInstanceConfigs(ctx context.Context, input *WriteInstanceConfigsInput) (*WriteInstanceConfigsOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	// paths := HostPathsFor(cfg, input.Spec)

	// // path := filepath.Join(cfg.DataDir, input.Name)
	// if err := a.Fs.MkdirAll(input.HostPaths.Configs.Dir, 0o700); err != nil {
	// 	return fmt.Errorf("failed to make configs directory: %w", err)
	// }
	// if err := a.Fs.MkdirAll(input.HostPaths.Certificates.Dir, 0o700); err != nil {
	// 	return fmt.Errorf("failed to make configs directory: %w", err)
	// }

	etcdCreds, err := a.Etcd.AddInstanceUser(ctx, etcd.InstanceUserOptions{
		InstanceID: input.Spec.InstanceID,
		KeyPrefix:  patroni.Namespace(input.Spec.DatabaseID, input.Spec.NodeName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add instance etcd user: %w", err)
	}

	pgServerPrincipal, err := a.CertService.PostgresServer(ctx,
		input.Spec.InstanceID,
		input.InstanceHostname,
		[]string{input.InstanceHostname, "localhost"},
		[]string{"127.0.0.1"},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create postgres server principal: %w", err)
	}
	pgSuperuserPrincipal, err := a.CertService.PostgresUser(ctx, input.Spec.InstanceID, "pgedge")
	if err != nil {
		return nil, fmt.Errorf("failed to create pgedge postgres user principal: %w", err)
	}
	pgReplicatorPrincipal, err := a.CertService.PostgresUser(ctx, input.Spec.InstanceID, "patroni_replicator")
	if err != nil {
		return nil, fmt.Errorf("failed to create patroni_replicator postgres user principal: %w", err)
	}

	certificates := filesystem.Directory{
		Path: input.HostPaths.Certificates.Dir,
		Mode: 0o700,
		Owner: &filesystem.Owner{
			User:  input.Owner.User,
			Group: input.Owner.Group,
		},
		Children: []filesystem.TreeNode{
			&filesystem.Directory{
				Path: "etcd",
				Mode: 0o700,
				Children: []filesystem.TreeNode{
					&filesystem.File{
						Path:     "ca.crt",
						Mode:     0o644,
						Contents: etcdCreds.CaCert,
					},
					&filesystem.File{
						Path:     "client.crt",
						Mode:     0o644,
						Contents: etcdCreds.ClientCert,
					},
					&filesystem.File{
						Path:     "client.key",
						Mode:     0o600,
						Contents: etcdCreds.ClientKey,
					},
				},
			},
			&filesystem.Directory{
				Path: "postgres",
				Mode: 0o700,
				Children: []filesystem.TreeNode{
					&filesystem.File{
						Path:     "ca.crt",
						Mode:     0o644,
						Contents: a.CertService.CACert(),
					},
					&filesystem.File{
						Path:     "server.crt",
						Mode:     0o644,
						Contents: pgServerPrincipal.CertPEM,
					},
					&filesystem.File{
						Path:     "server.key",
						Mode:     0o600,
						Contents: pgServerPrincipal.KeyPEM,
					},
					&filesystem.File{
						Path:     "superuser.crt",
						Mode:     0o644,
						Contents: pgSuperuserPrincipal.CertPEM,
					},
					&filesystem.File{
						Path:     "superuser.key",
						Mode:     0o600,
						Contents: pgSuperuserPrincipal.KeyPEM,
					},
					&filesystem.File{
						Path:     "patroni_replicator.crt",
						Mode:     0o644,
						Contents: pgReplicatorPrincipal.CertPEM,
					},
					&filesystem.File{
						Path:     "patroni_replicator.key",
						Mode:     0o600,
						Contents: pgReplicatorPrincipal.KeyPEM,
					},
				},
			},
		},
	}

	if err := certificates.Create(ctx, a.Fs, a.Run, ""); err != nil {
		return nil, fmt.Errorf("failed to create postgres certificates directory: %w", err)
	}

	// err = afero.WriteFile(a.Fs, input.HostPaths.Certificates.CACert, a.CertService.CACert(), 0o644)
	// if err != nil {
	// 	return fmt.Errorf("failed to write CA certificate: %w", err)
	// }
	// err = afero.WriteFile(a.Fs, input.HostPaths.Certificates.ServerCert, serverPrincipal.CertPEM, 0o600)
	// if err != nil {
	// 	return fmt.Errorf("failed to write server cert: %w", err)
	// }
	// err = afero.WriteFile(a.Fs, input.HostPaths.Certificates.ServerKey, serverPrincipal.KeyPEM, 0o600)
	// if err != nil {
	// 	return fmt.Errorf("failed to write server cert: %w", err)
	// }

	// err = afero.WriteFile(a.Fs, input.HostPaths.Certificates.SuperuserCert, superuserPrincipal.CertPEM, 0o600)
	// if err != nil {
	// 	return fmt.Errorf("failed to write superuser cert: %w", err)
	// }
	// err = afero.WriteFile(a.Fs, input.HostPaths.Certificates.SuperuserKey, superuserPrincipal.KeyPEM, 0o600)
	// if err != nil {
	// 	return fmt.Errorf("failed to write superuser key: %w", err)
	// }

	// err = afero.WriteFile(a.Fs, input.HostPaths.Certificates.PatroniReplicatorCert, replicatorPrincipal.CertPEM, 0o600)
	// if err != nil {
	// 	return fmt.Errorf("failed to write replicator cert: %w", err)
	// }
	// err = afero.WriteFile(a.Fs, input.HostPaths.Certificates.PatroniReplicatorKey, replicatorPrincipal.KeyPEM, 0o600)
	// if err != nil {
	// 	return fmt.Errorf("failed to write replicator key: %w", err)
	// }

	// Using the default bridge network to avoid this issue:
	// https://github.com/moby/moby/issues/37087
	bridge, err := a.Docker.NetworkInspect(ctx, "bridge", network.InspectOptions{
		Scope: "local",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect the default bridge network: %w", err)
	}
	bridgeInfo, err := docker.ExtractNetworkInfo(bridge)
	if err != nil {
		return nil, fmt.Errorf("failed to extract bridge network info: %w", err)
	}

	members, err := a.EtcdClient.MemberList(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list etcd cluster members: %w", err)
	}
	var endpoints []string
	for _, member := range members.Members {
		endpoints = append(endpoints, member.GetClientURLs()...)
	}

	patroniConfig, err := PatroniConfig(input, bridgeInfo, endpoints, etcdCreds.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to create Patroni config: %w", err)
	}

	// Remember that the YAML spec is a superset of the JSON spec, so JSON
	// is valid YAML.
	patroniYaml, err := json.MarshalIndent(patroniConfig, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Patroni YAML: %w", err)
	}

	configs := &filesystem.Directory{
		Path: input.HostPaths.Configs.Dir,
		Mode: 0o700,
		Owner: &filesystem.Owner{
			User:  input.Owner.User,
			Group: input.Owner.Group,
		},
		Children: []filesystem.TreeNode{
			&filesystem.File{
				Path:     "patroni.yaml",
				Mode:     0o644,
				Contents: patroniYaml,
				Owner:    &filesystem.Owner{User: input.Owner.User, Group: input.Owner.Group},
			},
		},
	}
	if err := configs.Create(ctx, a.Fs, a.Run, ""); err != nil {
		return nil, fmt.Errorf("failed to create configs directory: %w", err)
	}

	// err = afero.WriteFile(a.Fs, input.HostPaths.Configs.PatroniYAML, patroniYaml, 0o600)
	// if err != nil {
	// 	return fmt.Errorf("failed to write Patroni YAML: %w", err)
	// }

	// // This is safe to run every time because Owner.String() returns ":" if
	// // neither group nor user are specified.
	// if _, err := a.Run(ctx, "sudo", "chown", "-R", input.Owner.String(), input.HostPaths.Configs.Dir, input.HostPaths.Certificates.Dir); err != nil {
	// 	return fmt.Errorf("failed to change mount path ownership: %w", err)
	// }

	return &WriteInstanceConfigsOutput{}, nil
}
