package worker

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/backend"
	"github.com/cschleiden/go-workflows/worker"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/rs/zerolog"
	"github.com/spf13/afero"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/docker"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/exec"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/host"
	"github.com/pgEdge/control-plane/server/internal/ipam"
	"github.com/pgEdge/control-plane/server/internal/workflows"
	"github.com/pgEdge/control-plane/server/internal/workflows/activities/general"
	swarm_activities "github.com/pgEdge/control-plane/server/internal/workflows/activities/swarm"
	swarm_workflows "github.com/pgEdge/control-plane/server/internal/workflows/swarm"
)

type Worker struct {
	cfg       config.Config
	logger    zerolog.Logger
	w         *worker.Worker
	workflows any
}

func NewWorker(cfg config.Config, logger zerolog.Logger, be backend.Backend) *Worker {
	queues := []workflow.Queue{
		workflow.Queue(cfg.ClusterID.String()),
		workflow.Queue(cfg.HostID.String()),
	}

	opts := worker.DefaultOptions
	opts.WorkflowQueues = queues
	opts.ActivityQueues = queues
	w := worker.New(be, &opts)

	return &Worker{
		cfg:    cfg,
		logger: logger,
		w:      w,
	}
}

func (w *Worker) StartSwarmWorker(
	ctx context.Context,
	fs afero.Fs,
	loopMgr filesystem.LoopDeviceManager,
	ipamSvc *ipam.Service,
	hostSvc *host.Service,
	certSvc *certificates.Service,
	dockerClient *docker.Docker,
	etcdServer *etcd.EmbeddedEtcd,
	etcdClient *clientv3.Client,
	run exec.CmdRunner,
) error {
	g := &general.Activities{
		Fs:          fs,
		Run:         run,
		Etcd:        etcdServer,
		LoopMgr:     loopMgr,
		HostService: hostSvc,
	}
	wflows := &workflows.Workflows{
		SwarmWorkflows: &swarm_workflows.Workflows{
			General: g,
			Swarm: &swarm_activities.Activities{
				Fs:          fs,
				IPAM:        ipamSvc,
				Docker:      dockerClient,
				Etcd:        etcdServer,
				EtcdClient:  etcdClient,
				Run:         run,
				CertService: certSvc,
				HostService: hostSvc,
				Config:      w.cfg,
			},
		},
		GeneralActivities: g,
		Config:            w.cfg,
	}
	// wflows :=
	w.w.RegisterActivity(g.PartitionInstances)
	wflows.Register(w.w)
	w.workflows = wflows

	if err := w.w.Start(ctx); err != nil {
		return fmt.Errorf("failed to start worker: %w", err)
	}
	return nil
}

// func (w *Worker)

// func registerSwarmWorker(w *worker.Worker) {
// 	wflows := &swarm_workflows.Workflows{
// 		General: &general.Activities{},
// 		Swarm:   &swarm_activities.Activities{},
// 	}
// 	createDatabaseWorkflow := workflows.NewSwarmCreateDatabase()

// 	w.RegisterWorkflow(createDatabaseWorkflow, registry.WithName("CreateDatabaseWorkflow"))
// }
