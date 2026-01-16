package scheduler

import (
	"context"
	"errors"

	"github.com/go-co-op/gocron"
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/election"
)

var ErrNonLeader = errors.New("the elector is not leader")

var _ gocron.Elector = (*Elector)(nil)
var _ do.Shutdownable = (*Elector)(nil)

type Elector struct {
	candidate *election.Candidate
}

func NewElector(candidate *election.Candidate) *Elector {
	return &Elector{
		candidate: candidate,
	}
}

func (e *Elector) Start(ctx context.Context) error {
	return e.candidate.Start(ctx)
}

func (e *Elector) IsLeader(_ context.Context) error {
	if !e.candidate.IsLeader() {
		return ErrNonLeader
	}

	return nil
}

func (e *Elector) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), electionTTL/3)
	defer cancel()

	return e.candidate.Stop(ctx)
}

func (e *Elector) Error() <-chan error {
	return e.candidate.Error()
}
