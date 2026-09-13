package application

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/governance/domain"
)

var (
	ErrUnauthenticated = errors.New("governance authentication required")
	ErrForbidden       = errors.New("governance review forbidden")
	ErrInvalid         = errors.New("governance review invalid argument")
	ErrNotFound        = errors.New("governance approval not found")
	ErrConflict        = errors.New("governance approval conflict")
	ErrUnavailable     = errors.New("governance unavailable")
)

type Actor struct{ UserID string }

type PublicationApproval struct {
	WorkspaceID, ID, TargetID, RequesterUserID, ReviewerUserID, DecisionNote string
	TargetKind                                                               string
	TargetRevision                                                           int64
	State                                                                    string
	RequestedAt, ExpiresAt, ReviewedAt, ConsumedAt                           time.Time
}

type Authorizer interface {
	Authenticate(context.Context, string) (Actor, error)
	AuthenticateMutation(context.Context, string, string) (Actor, error)
	Authorize(context.Context, Actor, string, string) error
}

type Repository interface {
	ListPublicationApprovals(context.Context, string, time.Time) ([]PublicationApproval, error)
	ApprovePublication(context.Context, string, string, string, string, time.Time) (PublicationApproval, error)
	RejectPublication(context.Context, string, string, string, string, time.Time) (PublicationApproval, error)
}

type Clock interface{ Now() time.Time }

type Service struct {
	repository Repository
	auth       Authorizer
	clock      Clock
}

func New(repository Repository, auth Authorizer, clock Clock) (*Service, error) {
	if repository == nil || auth == nil || clock == nil {
		return nil, ErrUnavailable
	}
	return &Service{repository: repository, auth: auth, clock: clock}, nil
}

func validID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (s *Service) now() (time.Time, error) {
	at := s.clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() {
		return time.Time{}, ErrUnavailable
	}
	return at, nil
}

func asDomain(a PublicationApproval) domain.PublicationApproval {
	return domain.PublicationApproval{
		WorkspaceID: a.WorkspaceID, ID: a.ID, TargetKind: domain.PublicationTarget(a.TargetKind), TargetID: a.TargetID,
		TargetRevision: a.TargetRevision, RequesterUserID: a.RequesterUserID, ReviewerUserID: a.ReviewerUserID,
		State: domain.ApprovalState(a.State), DecisionNote: a.DecisionNote, RequestedAt: a.RequestedAt, ExpiresAt: a.ExpiresAt,
		ReviewedAt: a.ReviewedAt, ConsumedAt: a.ConsumedAt,
	}
}

func (s *Service) List(ctx context.Context, actor Actor, workspace string) ([]PublicationApproval, error) {
	if !validID(actor.UserID) || !validID(workspace) {
		return nil, ErrForbidden
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:review"); err != nil {
		return nil, err
	}
	at, err := s.now()
	if err != nil {
		return nil, err
	}
	items, err := s.repository.ListPublicationApprovals(ctx, workspace, at)
	if err != nil {
		return nil, err
	}
	for i := range items {
		projected := asDomain(items[i])
		if items[i].WorkspaceID != workspace || !projected.Valid() {
			return nil, ErrUnavailable
		}
		items[i].State = string(projected.EffectiveState(at))
	}
	return items, nil
}

func (s *Service) Approve(ctx context.Context, actor Actor, workspace, id, note string) (PublicationApproval, error) {
	return s.decide(ctx, actor, workspace, id, note, true)
}

func (s *Service) Reject(ctx context.Context, actor Actor, workspace, id, note string) (PublicationApproval, error) {
	return s.decide(ctx, actor, workspace, id, note, false)
}

func (s *Service) decide(ctx context.Context, actor Actor, workspace, id, note string, approve bool) (PublicationApproval, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(id) || len([]rune(note)) > 1000 {
		return PublicationApproval{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:review"); err != nil {
		return PublicationApproval{}, err
	}
	at, err := s.now()
	if err != nil {
		return PublicationApproval{}, err
	}
	var item PublicationApproval
	if approve {
		item, err = s.repository.ApprovePublication(ctx, workspace, id, actor.UserID, note, at)
	} else {
		item, err = s.repository.RejectPublication(ctx, workspace, id, actor.UserID, note, at)
	}
	if err != nil {
		return PublicationApproval{}, err
	}
	if item.WorkspaceID != workspace || item.ID != id || !asDomain(item).Valid() {
		return PublicationApproval{}, ErrUnavailable
	}
	expected := string(domain.ApprovalRejected)
	if approve {
		expected = string(domain.ApprovalApproved)
	}
	if item.State != expected {
		return PublicationApproval{}, ErrConflict
	}
	return item, nil
}
