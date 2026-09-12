package application

import (
	"context"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/connections/domain"
)

type HumanActor struct{ UserID string }

// HumanConnection is the application projection consumed by inbound adapters.
// Domain entities remain internal to the application/repository boundary.
type HumanConnection struct {
	WorkspaceID, ConnectionID, ProviderID, State string
	Revision                                     int64
	CreatedAt, ExpiresAt                         time.Time
}

type HumanAuthorizer interface {
	Authenticate(context.Context, string) (HumanActor, error)
	AuthenticateMutation(context.Context, string, string) (HumanActor, error)
	Authorize(context.Context, HumanActor, string, string) error
}

type HumanRepository interface {
	ListSummaries(context.Context, string) ([]domain.Summary, error)
	Revoke(context.Context, string, string, time.Time) (domain.Summary, error)
}

type HumanService struct {
	repository HumanRepository
	authorizer HumanAuthorizer
}

func NewHuman(repository HumanRepository, authorizer HumanAuthorizer) (*HumanService, error) {
	if repository == nil || authorizer == nil {
		return nil, ErrUnavailable
	}
	return &HumanService{repository: repository, authorizer: authorizer}, nil
}

func projectHumanConnection(item domain.Summary) HumanConnection {
	return HumanConnection{
		WorkspaceID:  item.WorkspaceID,
		ConnectionID: item.ConnectionID,
		ProviderID:   item.ProviderID,
		State:        item.State,
		Revision:     item.Revision,
		CreatedAt:    item.CreatedAt,
		ExpiresAt:    item.ExpiresAt,
	}
}

func (s *HumanService) List(ctx context.Context, actor HumanActor, workspace string) ([]HumanConnection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.authorizer.Authorize(ctx, actor, workspace, "connection:read"); err != nil {
		return nil, err
	}
	items, err := s.repository.ListSummaries(ctx, workspace)
	if err != nil {
		return nil, err
	}
	result := make([]HumanConnection, 0, len(items))
	for _, item := range items {
		if item.WorkspaceID != workspace || item.Validate() != nil {
			return nil, ErrUnavailable
		}
		result = append(result, projectHumanConnection(item))
	}
	return result, nil
}

func (s *HumanService) Revoke(ctx context.Context, actor HumanActor, workspace, connectionID string, at time.Time) (HumanConnection, error) {
	if err := ctx.Err(); err != nil {
		return HumanConnection{}, err
	}
	if at.IsZero() || connectionID == "" {
		return HumanConnection{}, ErrForbidden
	}
	if err := s.authorizer.Authorize(ctx, actor, workspace, "connection:manage"); err != nil {
		return HumanConnection{}, err
	}
	item, err := s.repository.Revoke(ctx, workspace, connectionID, at.UTC().Truncate(time.Microsecond))
	if err != nil {
		return HumanConnection{}, err
	}
	if item.WorkspaceID != workspace || item.ConnectionID != connectionID || item.State != "revoked" || item.Validate() != nil {
		return HumanConnection{}, ErrUnavailable
	}
	return projectHumanConnection(item), nil
}
