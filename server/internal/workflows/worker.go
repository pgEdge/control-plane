package workflows

import (
	"context"
	"fmt"
	"time"

	"github.com/cschleiden/go-workflows/backend"
	"github.com/cschleiden/go-workflows/worker"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/rs/zerolog"
	"github.com/samber/do"
)

var _ do.Shutdownable = (*Worker)(nil)

type Orchestrator interface {
	WorkerQueues() ([]workflow.Queue, error)
}

type Worker struct {
	logger    zerolog.Logger
	worker    *worker.Worker
	workflows *Workflows
	ctx       context.Context
	cancel    context.CancelFunc
}

func NewWorker(logger zerolog.Logger, be backend.Backend, workflows *Workflows, orch Orchestrator) (*Worker, error) {
	queues, err := orch.WorkerQueues()
	if err != nil {
		return nil, fmt.Errorf("failed to get worker queues: %w", err)
	}

	opts := worker.DefaultOptions
	opts.WorkflowQueues = queues
	opts.ActivityQueues = queues
	opts.ActivityPollingInterval = 500 * time.Millisecond
	opts.WorkflowPollingInterval = 500 * time.Millisecond
	w := worker.New(be, &opts)

	if err := workflows.Register(w); err != nil {
		return nil, fmt.Errorf("failed to register workflows: %w", err)
	}

	return &Worker{
		logger: logger.With().
			Str("component", "workflows_worker").
			Logger(),
		worker:    w,
		workflows: workflows,
	}, nil
}

func (w *Worker) Start(ctx context.Context) error {
	if w.cancel != nil {
		return fmt.Errorf("workflows worker already started")
	}

	w.logger.Debug().Msg("starting workflows worker")

	// The parent context isn't canceled until the stop grace period times out,
	// so we start the worker with a child context that we can cancel ourselves.
	childCtx, cancel := context.WithCancel(ctx)

	if err := w.worker.Start(childCtx); err != nil {
		cancel()
		return fmt.Errorf("failed to start worker: %w", err)
	}
	w.ctx = childCtx
	w.cancel = cancel
	return nil
}

func (w *Worker) Shutdown() error {
	w.logger.Debug().Msg("shutting down workflow worker")

	if w.cancel != nil {
		w.cancel()
	}

	if err := w.worker.WaitForCompletion(); err != nil {
		return fmt.Errorf("failed to wait for active tasks to complete: %w", err)
	}
	return nil
}
