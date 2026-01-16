package election

import (
	"time"

	"github.com/rs/zerolog"
)

// Service manages election operations.
type Service struct {
	store  *ElectionStore
	logger zerolog.Logger
}

// NewService returns a new Service.
func NewService(
	store *ElectionStore,
	logger zerolog.Logger,
) *Service {
	return &Service{
		store:  store,
		logger: logger,
	}
}

// NewCandidate creates a new Candidate for the given election. candidateID must
// be unique amongst candidates.
func (s *Service) NewCandidate(electionName Name, candidateID string, ttl time.Duration, onClaim ...ClaimHandler) *Candidate {
	return NewCandidate(s.store, s.logger, electionName, candidateID, ttl, onClaim)
}
