package application

import (
	"context"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/governance/domain"
)

type PluginPublicationApproval struct {
	WorkspaceID, ID, PluginID, Version, RequesterUserID, ReviewerUserID, State, DecisionNote string
	TargetRevision                                                                           int64
	RequestedAt, ExpiresAt, ReviewedAt, ConsumedAt                                           time.Time
}

type PluginPublicationReviewRepository interface {
	ListPluginPublicationApprovals(context.Context, string, time.Time) ([]PluginPublicationApproval, error)
	ApprovePluginPublication(context.Context, string, string, string, string, time.Time) (PluginPublicationApproval, error)
	RejectPluginPublication(context.Context, string, string, string, string, time.Time) (PluginPublicationApproval, error)
}

type PluginPublicationReview struct {
	repository PluginPublicationReviewRepository
	auth       Authorizer
	clock      Clock
}

func NewPluginPublicationReview(repository PluginPublicationReviewRepository, auth Authorizer, clock Clock) (*PluginPublicationReview, error) {
	if repository == nil || auth == nil || clock == nil {
		return nil, ErrUnavailable
	}
	return &PluginPublicationReview{repository: repository, auth: auth, clock: clock}, nil
}

func asPluginDomain(a PluginPublicationApproval) domain.PluginPublicationApproval {
	return domain.PluginPublicationApproval{
		WorkspaceID: a.WorkspaceID, ID: a.ID, PluginID: a.PluginID, Version: a.Version,
		TargetRevision: a.TargetRevision, RequesterUserID: a.RequesterUserID, ReviewerUserID: a.ReviewerUserID,
		State: domain.ApprovalState(a.State), DecisionNote: a.DecisionNote, RequestedAt: a.RequestedAt,
		ExpiresAt: a.ExpiresAt, ReviewedAt: a.ReviewedAt, ConsumedAt: a.ConsumedAt,
	}
}

func (s *PluginPublicationReview) now() (time.Time, error) {
	at := s.clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() {
		return time.Time{}, ErrUnavailable
	}
	return at, nil
}

func (s *PluginPublicationReview) List(ctx context.Context, actor Actor, workspace string) ([]PluginPublicationApproval, error) {
	if !validID(actor.UserID) || !validID(workspace) {
		return nil, ErrForbidden
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "publisher:review"); err != nil {
		return nil, err
	}
	at, err := s.now()
	if err != nil {
		return nil, err
	}
	items, err := s.repository.ListPluginPublicationApprovals(ctx, workspace, at)
	if err != nil {
		return nil, err
	}
	for i := range items {
		projected := asPluginDomain(items[i])
		if items[i].WorkspaceID != workspace || !projected.Valid() {
			return nil, ErrUnavailable
		}
		items[i].State = string(projected.EffectiveState(at))
	}
	return items, nil
}

func (s *PluginPublicationReview) Approve(ctx context.Context, actor Actor, workspace, id, note string) (PluginPublicationApproval, error) {
	return s.decide(ctx, actor, workspace, id, note, true)
}

func (s *PluginPublicationReview) Reject(ctx context.Context, actor Actor, workspace, id, note string) (PluginPublicationApproval, error) {
	return s.decide(ctx, actor, workspace, id, note, false)
}

func (s *PluginPublicationReview) decide(ctx context.Context, actor Actor, workspace, id, note string, approve bool) (PluginPublicationApproval, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(id) || len([]rune(note)) > 1000 {
		return PluginPublicationApproval{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "publisher:review"); err != nil {
		return PluginPublicationApproval{}, err
	}
	at, err := s.now()
	if err != nil {
		return PluginPublicationApproval{}, err
	}
	var item PluginPublicationApproval
	if approve {
		item, err = s.repository.ApprovePluginPublication(ctx, workspace, id, actor.UserID, note, at)
	} else {
		item, err = s.repository.RejectPluginPublication(ctx, workspace, id, actor.UserID, note, at)
	}
	if err != nil {
		return PluginPublicationApproval{}, err
	}
	if item.WorkspaceID != workspace || item.ID != id || !asPluginDomain(item).Valid() {
		return PluginPublicationApproval{}, ErrUnavailable
	}
	expected := string(domain.ApprovalRejected)
	if approve {
		expected = string(domain.ApprovalApproved)
	}
	if item.State != expected {
		return PluginPublicationApproval{}, ErrConflict
	}
	return item, nil
}
