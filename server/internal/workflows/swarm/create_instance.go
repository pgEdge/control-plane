package swarm

import (
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/dustin/go-humanize"
	"github.com/google/uuid"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities/general"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities/swarm"
)

type CreateInstanceInput struct {
	Host     *host.Host             `json:"host"`
	Manager  *host.Host             `json:"manager"`
	Instance *database.InstanceSpec `json:"instance"`
	// BridgeNetwork   swarm.NetworkInfo      `json:"bridge_network"`
	DatabaseNetwork swarm.NetworkInfo `json:"database_network"`
	ClusterSize     int               `json:"cluster_size"`
}

// TODO: should have instance info, including container ID, IP address, patroni info, etc.
type CreateInstanceOutput struct {
}

func (w *Workflows) ExecuteCreateInstance(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *CreateInstanceInput,
) workflow.Future[*CreateInstanceOutput] {
	options := workflow.SubWorkflowOptions{
		Queue: core.Queue(hostID.String()),
	}
	return workflow.CreateSubWorkflowInstance[*CreateInstanceOutput](ctx, options, w.CreateInstance, input)
}

func (w *Workflows) CreateInstance(ctx workflow.Context, input *CreateInstanceInput) error {
	logger := workflow.Logger(ctx).With(
		"instance_id", input.Instance.InstanceID.String(),
		"host_id", input.Host.ID.String(),
		"manager_id", input.Manager.ID.String(),
	)
	logger.Info("creating instance")

	paths := swarm.HostPathsFor(w.Swarm.Config, input.Instance)
	owner := general.Owner{
		User:  "1020",
		Group: "1020",
	}

	// TODO: find a better place to apply the default storage class

	switch input.Instance.StorageClass {
	case "", "local":
		_, err := w.General.ExecuteCreateDataDir(ctx, input.Instance.HostID, &general.CreateDataDirInput{
			DataDir: paths.Data.Dir,
			Owner:   owner,
		}).Get(ctx)
		if err != nil {
			return fmt.Errorf("failed to create data directory: %w", err)
		}
	case "loop_device":
		if w.General.LoopMgr == nil {
			return fmt.Errorf("loop_device storage class is not available on this host")
		}
		if input.Instance.StorageSizeBytes == 0 {
			return fmt.Errorf("storage size is required for 'loop_device' storage class")
		}
		_, err := w.General.ExecuteCreateLoopDevice(ctx, input.Instance.HostID, &general.CreateLoopDeviceInput{
			DataDir:  paths.Data.Dir,
			SizeSpec: humanize.Bytes(input.Instance.StorageSizeBytes),
			Owner:    owner,
		}).Get(ctx)
		if err != nil {
			return fmt.Errorf("failed to create loop device: %w", err)
		}
	default:
		return fmt.Errorf("unsupported storage class: %q", input.Instance.StorageClass)
	}

	// _, err := w.Swarm.ExecuteCreateBridgeConfig(ctx, input.Host.ID, &swarm.CreateBridgeConfigInput{
	// 	DatabaseID: input.Instance.DatabaseID,
	// 	Subnet:     input.BridgeNetwork.Subnet,
	// 	Gateway:    input.BridgeNetwork.Gateway,
	// }).Get(ctx)
	// if err != nil {
	// 	return fmt.Errorf("failed to create etcd user: %w", err)
	// }

	_, err := w.Swarm.ExecuteWriteInstanceConfigs(ctx, input.Host.ID, &swarm.WriteInstanceConfigsInput{
		Host:             input.Host,
		HostPaths:        paths,
		Spec:             input.Instance,
		InstanceHostname: "postgres-" + input.Instance.NodeName,
		DatabaseNetwork:  input.DatabaseNetwork,
		Owner:            owner,
	}).Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to write instance configs: %w", err)
	}

	swarmService, err := w.Swarm.ExecuteGetServiceSpec(ctx, input.Host.ID, &swarm.GetServiceSpecInput{
		Instance: input.Instance,
		// BridgeNetwork:   input.BridgeNetwork,
		DatabaseNetwork: input.DatabaseNetwork,
	}).Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get service spec: %w", err)
	}

	_, err = w.Swarm.ExecuteCreateDBService(ctx, input.Manager.ID, &swarm.CreateDBServiceInput{
		Service: swarmService.Service,
	}).Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to create database service: %w", err)
	}

	if input.Instance.EnableBackups && input.Instance.UsesPgBackRest() {
		_, err = w.Swarm.ExecuteCreatePgBackRestStanza(ctx, input.Host.ID, &swarm.CreatePgBackRestStanzaInput{
			Instance: input.Instance,
		}).Get(ctx)
		if err != nil {
			return fmt.Errorf("failed to create pgBackRest stanza: %w", err)
		}
	}

	logger.Info("successfully created instance")

	// TODO: should gather instance info from running container and wait for
	// patroni to say that it's healthy.

	return nil
}
