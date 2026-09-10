package application

import (
	"context"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/connections/domain"
)

type RuntimeRepository interface {
	FindRuntimeCredential(context.Context, string, string, string) (domain.RuntimeCredential, error)
}

type RuntimeService struct{ repository RuntimeRepository }

func NewRuntimeService(repository RuntimeRepository) (*RuntimeService, error) {
	if repository == nil {
		return nil, ErrUnavailable
	}
	return &RuntimeService{repository: repository}, nil
}

func (s *RuntimeService) Resolve(ctx context.Context, workspace, subject, connectionID, providerID string, at time.Time) (domain.RuntimeCredential, error) {
	if err := ctx.Err(); err != nil {
		return domain.RuntimeCredential{}, err
	}
	credential, err := s.repository.FindRuntimeCredential(ctx, workspace, subject, connectionID)
	if err != nil {
		return domain.RuntimeCredential{}, err
	}
	if credential.Validate() != nil || !credential.AllowedAt(at) || credential.WorkspaceID != workspace || credential.SubjectID != subject || credential.ConnectionID != connectionID || credential.ProviderID != providerID {
		return domain.RuntimeCredential{}, ErrForbidden
	}
	return credential, nil
}
