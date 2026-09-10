package application

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/catalog/domain"
)

var (
	ErrNotFound    = errors.New("tool version not found")
	ErrUnavailable = errors.New("catalog unavailable")
)

type Repository interface {
	FindToolVersion(context.Context, string) (domain.ToolVersion, error)
}

type Service struct{ repository Repository }

func New(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, ErrUnavailable
	}
	return &Service{repository: repository}, nil
}

func (s *Service) Resolve(ctx context.Context, id, toolID, version string) (domain.ToolVersion, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolVersion{}, err
	}
	v, err := s.repository.FindToolVersion(ctx, id)
	if err != nil {
		return domain.ToolVersion{}, err
	}
	if !v.Callable() {
		return domain.ToolVersion{}, ErrNotFound
	}
	if v.ID != id || v.ToolID != toolID || v.Version != version {
		return domain.ToolVersion{}, ErrNotFound
	}
	return v, nil
}
