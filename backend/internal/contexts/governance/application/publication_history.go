package application

import (
	"context"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/governance/domain"
)

type PublicationHistoryFilter struct {
	TargetKind     string
	TargetID       string
	ApprovalID     string
	EventKind      string
	BeforeSequence int64
	Limit          int
}

type PublicationAuditEvent struct {
	Sequence                          int64
	WorkspaceID, ApprovalID, TargetID string
	TargetKind                        string
	TargetRevision, ObservedRevision  int64
	EventKind                         string
	ActorUserID, ReasonCode, Note     string
	OccurredAt                        time.Time
}

type PublicationHistoryPage struct {
	Events             []PublicationAuditEvent
	NextBeforeSequence int64
}

type HistoryRepository interface {
	ListPublicationHistory(context.Context, string, PublicationHistoryFilter) (PublicationHistoryPage, error)
}

type HistoryService struct {
	repository HistoryRepository
	auth       Authorizer
}

func NewHistory(repository HistoryRepository, auth Authorizer) (*HistoryService, error) {
	if repository == nil || auth == nil {
		return nil, ErrUnavailable
	}
	return &HistoryService{repository: repository, auth: auth}, nil
}

func validAuditEventKind(value string) bool {
	switch domain.PublicationAuditKind(value) {
	case domain.AuditBaseline, domain.AuditApprovalSubmitted, domain.AuditApprovalApproved, domain.AuditApprovalRejected,
		domain.AuditApprovalExpired, domain.AuditApprovalConsumed, domain.AuditPublicationCommitted, domain.AuditPublicationRetired:
		return true
	default:
		return false
	}
}

func validHistoryFilter(filter PublicationHistoryFilter) bool {
	if filter.TargetKind != "" && filter.TargetKind != string(domain.TargetToolVersion) && filter.TargetKind != string(domain.TargetToolset) {
		return false
	}
	if filter.TargetID != "" && !validID(filter.TargetID) {
		return false
	}
	if filter.ApprovalID != "" && !validID(filter.ApprovalID) {
		return false
	}
	if filter.EventKind != "" && !validAuditEventKind(filter.EventKind) {
		return false
	}
	return filter.BeforeSequence >= 0 && filter.Limit >= 1 && filter.Limit <= 100
}

func asDomainAudit(event PublicationAuditEvent) domain.PublicationAuditEvent {
	return domain.PublicationAuditEvent{
		Sequence: event.Sequence, WorkspaceID: event.WorkspaceID, ApprovalID: event.ApprovalID,
		TargetKind: domain.PublicationTarget(event.TargetKind), TargetID: event.TargetID,
		TargetRevision: event.TargetRevision, ObservedRevision: event.ObservedRevision,
		EventKind: domain.PublicationAuditKind(event.EventKind), ActorUserID: event.ActorUserID,
		OccurredAt: event.OccurredAt, ReasonCode: event.ReasonCode, Note: event.Note,
	}
}

func (s *HistoryService) List(ctx context.Context, actor Actor, workspace string, filter PublicationHistoryFilter) (PublicationHistoryPage, error) {
	if !validID(actor.UserID) || !validID(workspace) {
		return PublicationHistoryPage{}, ErrForbidden
	}
	if !validHistoryFilter(filter) {
		return PublicationHistoryPage{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:audit"); err != nil {
		return PublicationHistoryPage{}, err
	}
	page, err := s.repository.ListPublicationHistory(ctx, workspace, filter)
	if err != nil {
		return PublicationHistoryPage{}, err
	}
	if len(page.Events) > filter.Limit || page.NextBeforeSequence < 0 {
		return PublicationHistoryPage{}, ErrUnavailable
	}
	var previous int64
	for i, event := range page.Events {
		if event.WorkspaceID != workspace || !asDomainAudit(event).Valid() {
			return PublicationHistoryPage{}, ErrUnavailable
		}
		if i > 0 && event.Sequence >= previous {
			return PublicationHistoryPage{}, ErrUnavailable
		}
		if filter.BeforeSequence > 0 && event.Sequence >= filter.BeforeSequence {
			return PublicationHistoryPage{}, ErrUnavailable
		}
		if filter.TargetKind != "" && event.TargetKind != filter.TargetKind ||
			filter.TargetID != "" && event.TargetID != filter.TargetID ||
			filter.ApprovalID != "" && event.ApprovalID != filter.ApprovalID ||
			filter.EventKind != "" && event.EventKind != filter.EventKind {
			return PublicationHistoryPage{}, ErrUnavailable
		}
		previous = event.Sequence
	}
	if page.NextBeforeSequence > 0 {
		if len(page.Events) == 0 || page.NextBeforeSequence != page.Events[len(page.Events)-1].Sequence {
			return PublicationHistoryPage{}, ErrUnavailable
		}
	}
	return page, nil
}
