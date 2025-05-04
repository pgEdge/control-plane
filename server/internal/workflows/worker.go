package workflows

import (
	"context"
	"fmt"

	"github.com/cschleiden/go-workflows/backend"
	"github.com/cschleiden/go-workflows/worker"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/samber/do"
)

var _ do.Shutdownable = (*Worker)(nil)

type Orchestrator interface {
	WorkerQueues() ([]workflow.Queue, error)
}

type Worker struct {
	worker    *worker.Worker
	workflows *Workflows
}

func NewWorker(be backend.Backend, workflows *Workflows, orch Orchestrator) (*Worker, error) {
	queues, err := orch.WorkerQueues()
	if err != nil {
		return nil, fmt.Errorf("failed to get worker queues: %w", err)
	}

	opts := worker.DefaultOptions
	opts.WorkflowQueues = queues
	opts.ActivityQueues = queues
	w := worker.New(be, &opts)

	if err := workflows.Register(w); err != nil {
		return nil, fmt.Errorf("failed to register workflows: %w", err)
	}

	return &Worker{
		worker:    w,
		workflows: workflows,
	}, nil
}

func (w *Worker) Start(ctx context.Context) error {
	if err := w.worker.Start(ctx); err != nil {
		return fmt.Errorf("failed to start worker: %w", err)
	}
	return nil
}

func (w *Worker) Shutdown() error {
	if err := w.worker.WaitForCompletion(); err != nil {
		return fmt.Errorf("failed to wait for active tasks to complete: %w", err)
	}
	return nil
}
