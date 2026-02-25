package election

import (
	"time"

	"github.com/pgEdge/control-plane/server/internal/logging"
)

// Service manages election operations.
type Service struct {
	store         *ElectionStore
	loggerFactory *logging.Factory
}

// NewService returns a new Service.
func NewService(
	store *ElectionStore,
	loggerFactory *logging.Factory,
) *Service {
	return &Service{
		store:         store,
		loggerFactory: loggerFactory,
	}
}

// NewCandidate creates a new Candidate for the given election. candidateID must
// be unique amongst candidates.
func (s *Service) NewCandidate(electionName Name, candidateID string, ttl time.Duration, onClaim ...ClaimHandler) *Candidate {
	return NewCandidate(s.store, s.loggerFactory, electionName, candidateID, ttl, onClaim)
}
