package application

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/connections/domain"
)

var (
	ErrForbidden   = errors.New("connection access denied")
	ErrUnavailable = errors.New("connections unavailable")
)

type Repository interface {
	FindAccess(context.Context, string, string, string) (domain.Access, error)
}

type Service struct{ repository Repository }

func New(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, ErrUnavailable
	}
	return &Service{repository: repository}, nil
}

func (s *Service) Resolve(ctx context.Context, workspace, subject, connectionID, providerID string, at time.Time) (domain.Access, error) {
	if err := ctx.Err(); err != nil {
		return domain.Access{}, err
	}
	a, err := s.repository.FindAccess(ctx, workspace, subject, connectionID)
	if err != nil {
		return domain.Access{}, err
	}
	if !a.AllowedAt(at) || a.WorkspaceID != workspace || a.SubjectID != subject || a.ConnectionID != connectionID || a.ProviderID != providerID {
		return domain.Access{}, ErrForbidden
	}
	return a, nil
}
