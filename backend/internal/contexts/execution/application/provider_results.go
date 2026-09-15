package application

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

var (
	ErrProviderResultUnavailable = errors.New("provider result storage unavailable")
	ErrProviderResultConflict    = errors.New("provider result conflicts with execution state")
	ErrProviderAlreadyTerminal   = errors.New("provider result is already terminal")
)

type ProviderResultRecord struct {
	Run         domain.Snapshot
	Job         domain.JobSnapshot
	Observation domain.ProviderObservation
}

func (s *ProviderResults) ObserveAgentInputRequest(ctx context.Context, request AgentInputRequest) (AgentInputRequestRecord, error) {
	if err := ctx.Err(); err != nil {
		return AgentInputRequestRecord{}, err
	}
	if request.Validate() != nil {
		return AgentInputRequestRecord{}, ErrAgentInputInvalid
	}
	repository, ok := s.repository.(AgentInputRequestRepository)
	if !ok {
		return AgentInputRequestRecord{}, ErrAgentInputUnavailable
	}
	return repository.RecordAgentInputRequest(ctx, request)
}

type ProviderResultRepository interface {
	RecordProviderObservation(context.Context, domain.ProviderObservation) (ProviderResultRecord, error)
}

type ProviderResults struct{ repository ProviderResultRepository }

func NewProviderResults(repository ProviderResultRepository) (*ProviderResults, error) {
	if repository == nil {
		return nil, ErrProviderResultUnavailable
	}
	return &ProviderResults{repository: repository}, nil
}

func (s *ProviderResults) Observe(ctx context.Context, observation domain.ProviderObservation) (ProviderResultRecord, error) {
	if err := ctx.Err(); err != nil {
		return ProviderResultRecord{}, err
	}
	if observation.Validate() != nil {
		return ProviderResultRecord{}, domain.ErrInvalidProviderObservation
	}
	return s.repository.RecordProviderObservation(ctx, observation)
}
