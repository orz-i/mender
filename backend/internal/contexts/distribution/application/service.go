package application

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/distribution/domain"
)

var (
	ErrNotFound    = errors.New("toolset binding not found")
	ErrUnavailable = errors.New("distribution unavailable")
)

type Repository interface {
	FindBinding(context.Context, string, string, string, string) (domain.Binding, error)
}

type Service struct{ repository Repository }

func New(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, ErrUnavailable
	}
	return &Service{repository: repository}, nil
}

func (s *Service) Resolve(ctx context.Context, workspace, toolsetVersionID, toolID, version string) (domain.Binding, error) {
	if err := ctx.Err(); err != nil {
		return domain.Binding{}, err
	}
	b, err := s.repository.FindBinding(ctx, workspace, toolsetVersionID, toolID, version)
	if err != nil {
		return domain.Binding{}, err
	}
	if !b.Callable() || b.WorkspaceID != workspace || b.ToolsetVersionID != toolsetVersionID || b.ToolID != toolID || b.ToolVersionLabel != version {
		return domain.Binding{}, ErrNotFound
	}
	return b, nil
}
