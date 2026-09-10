package application

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

var (
	ErrRuntimeInputNotFound    = errors.New("execution runtime input not found")
	ErrRuntimeInputUnavailable = errors.New("execution runtime input unavailable")
)

type RuntimeInputRepository interface {
	LoadRuntimeInput(context.Context, domain.WorkspaceID, domain.RunID) (domain.RuntimeInput, error)
}

type RuntimeInputService struct{ repository RuntimeInputRepository }

func NewRuntimeInputService(repository RuntimeInputRepository) (*RuntimeInputService, error) {
	if repository == nil {
		return nil, ErrRuntimeInputUnavailable
	}
	return &RuntimeInputService{repository: repository}, nil
}

func (s *RuntimeInputService) Resolve(ctx context.Context, workspace, run string) (domain.RuntimeInput, error) {
	if err := ctx.Err(); err != nil {
		return domain.RuntimeInput{}, err
	}
	w := domain.WorkspaceID(workspace)
	r := domain.RunID(run)
	if !w.IsValid() || !r.IsValid() {
		return domain.RuntimeInput{}, ErrRuntimeInputNotFound
	}
	input, err := s.repository.LoadRuntimeInput(ctx, w, r)
	if err != nil {
		return domain.RuntimeInput{}, err
	}
	if input.WorkspaceID != w || input.RunID != r || input.Validate() != nil {
		return domain.RuntimeInput{}, ErrRuntimeInputUnavailable
	}
	return input, nil
}
