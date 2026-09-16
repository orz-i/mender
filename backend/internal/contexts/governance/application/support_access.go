package application

import (
	"context"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/governance/domain"
)

const supportApprovalTTL = 15 * time.Minute

type PlatformAuthorizer interface {
	Authenticate(context.Context, string) (Actor, error)
	AuthenticateMutation(context.Context, string, string) (Actor, error)
	AuthorizePlatform(context.Context, Actor, string) error
}

type JITSupportGrant struct {
	WorkspaceID, ID, ApprovalID, UserID, Reason string
	Scopes                                      []string
	CreatedAt, ExpiresAt, RevokedAt             time.Time
}

type SupportRun struct {
	WorkspaceID, ID, State string
	Version                int64
	CreatedAt, UpdatedAt   time.Time
}

type SupportAccessIDs interface {
	NewDangerousOperationID() (string, error)
	NewJITGrantID() (string, error)
}

type SupportManagementRepository interface {
	RequestSupportJIT(context.Context, string, string, string, []string, time.Duration, string, time.Time, time.Time) (DangerousOperationApproval, error)
	ApproveDangerousOperation(context.Context, string, string, string, time.Time, string) (DangerousOperationApproval, error)
	RejectDangerousOperation(context.Context, string, string, string, time.Time, string) (DangerousOperationApproval, error)
	ActivateJITSupport(context.Context, string, string, string, string, time.Time) (JITSupportGrant, error)
	RevokeJITSupport(context.Context, string, string, string, time.Time, string) (JITSupportGrant, error)
}

type SupportReadRepository interface {
	ListSupportRuns(context.Context, string, string, time.Time) ([]SupportRun, error)
}

type SupportAccessService struct {
	management SupportManagementRepository
	reader     SupportReadRepository
	auth       PlatformAuthorizer
	ids        SupportAccessIDs
	clock      Clock
}

func NewSupportAccessService(management SupportManagementRepository, reader SupportReadRepository, auth PlatformAuthorizer, ids SupportAccessIDs, clock Clock) (*SupportAccessService, error) {
	if management == nil || reader == nil || auth == nil || ids == nil || clock == nil {
		return nil, ErrUnavailable
	}
	return &SupportAccessService{management: management, reader: reader, auth: auth, ids: ids, clock: clock}, nil
}

func supportScopesValid(scopes []string) bool {
	if len(scopes) < 1 || len(scopes) > 3 {
		return false
	}
	seen := map[string]bool{}
	for _, scope := range scopes {
		if scope != domain.SupportScopeWorkspaceRead && scope != domain.SupportScopeRunRead && scope != domain.SupportScopeUsageRead || seen[scope] {
			return false
		}
		seen[scope] = true
	}
	return true
}

func (s *SupportAccessService) supportNow() (time.Time, error) {
	at := s.clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() || at.Year() > 9999 {
		return time.Time{}, ErrUnavailable
	}
	return at, nil
}

func (s *SupportAccessService) Request(ctx context.Context, actor Actor, workspace string, scopes []string, ttl time.Duration, reason string) (DangerousOperationApproval, error) {
	if !validID(actor.UserID) || !validID(workspace) || !supportScopesValid(scopes) || ttl < domain.MinSupportAccessTTL || ttl > domain.MaxSupportAccessTTL || len([]rune(reason)) < 1 || len([]rune(reason)) > 1000 {
		return DangerousOperationApproval{}, ErrInvalid
	}
	if err := s.auth.AuthorizePlatform(ctx, actor, "support:request"); err != nil {
		return DangerousOperationApproval{}, err
	}
	id, err := s.ids.NewDangerousOperationID()
	if err != nil || !validID(id) {
		return DangerousOperationApproval{}, ErrUnavailable
	}
	at, err := s.supportNow()
	if err != nil {
		return DangerousOperationApproval{}, err
	}
	item, err := s.management.RequestSupportJIT(ctx, workspace, id, actor.UserID, scopes, ttl, reason, at, at.Add(supportApprovalTTL))
	if err != nil {
		return DangerousOperationApproval{}, err
	}
	if item.ID != id || item.RequesterUserID != actor.UserID || item.Action != domain.DangerousActionSupportWorkspaceRead || item.TargetID != workspace || !validDangerousProjection(item, workspace) {
		return DangerousOperationApproval{}, ErrUnavailable
	}
	return item, nil
}

func (s *SupportAccessService) Approve(ctx context.Context, actor Actor, workspace, id, note string) (DangerousOperationApproval, error) {
	return s.review(ctx, actor, workspace, id, note, true)
}

func (s *SupportAccessService) Reject(ctx context.Context, actor Actor, workspace, id, note string) (DangerousOperationApproval, error) {
	return s.review(ctx, actor, workspace, id, note, false)
}

func (s *SupportAccessService) review(ctx context.Context, actor Actor, workspace, id, note string, approve bool) (DangerousOperationApproval, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(id) || len([]rune(note)) > 1000 {
		return DangerousOperationApproval{}, ErrInvalid
	}
	if err := s.auth.AuthorizePlatform(ctx, actor, "dangerous:review"); err != nil {
		return DangerousOperationApproval{}, err
	}
	at, err := s.supportNow()
	if err != nil {
		return DangerousOperationApproval{}, err
	}
	var item DangerousOperationApproval
	if approve {
		item, err = s.management.ApproveDangerousOperation(ctx, workspace, id, actor.UserID, at, note)
	} else {
		item, err = s.management.RejectDangerousOperation(ctx, workspace, id, actor.UserID, at, note)
	}
	if err != nil {
		return DangerousOperationApproval{}, err
	}
	if item.ID != id || item.Action != domain.DangerousActionSupportWorkspaceRead || !validDangerousProjection(item, workspace) {
		return DangerousOperationApproval{}, ErrUnavailable
	}
	return item, nil
}

func validJITGrant(value JITSupportGrant, workspace, user string) bool {
	domainValue := domain.JITSupportGrant{WorkspaceID: value.WorkspaceID, ID: value.ID, ApprovalID: value.ApprovalID, UserID: value.UserID, Reason: value.Reason, Scopes: value.Scopes, CreatedAt: value.CreatedAt, ExpiresAt: value.ExpiresAt, RevokedAt: value.RevokedAt}
	return value.WorkspaceID == workspace && value.UserID == user && domainValue.Valid()
}

func (s *SupportAccessService) Activate(ctx context.Context, actor Actor, workspace, approvalID string) (JITSupportGrant, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(approvalID) {
		return JITSupportGrant{}, ErrInvalid
	}
	if err := s.auth.AuthorizePlatform(ctx, actor, "support:request"); err != nil {
		return JITSupportGrant{}, err
	}
	id, err := s.ids.NewJITGrantID()
	if err != nil || !validID(id) {
		return JITSupportGrant{}, ErrUnavailable
	}
	at, err := s.supportNow()
	if err != nil {
		return JITSupportGrant{}, err
	}
	grant, err := s.management.ActivateJITSupport(ctx, workspace, approvalID, id, actor.UserID, at)
	if err != nil {
		return JITSupportGrant{}, err
	}
	if grant.ID != id || grant.ApprovalID != approvalID || !validJITGrant(grant, workspace, actor.UserID) {
		return JITSupportGrant{}, ErrUnavailable
	}
	return grant, nil
}

func (s *SupportAccessService) Revoke(ctx context.Context, actor Actor, workspace, grantID, reason string) (JITSupportGrant, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(grantID) || len([]rune(reason)) < 1 || len([]rune(reason)) > 1000 {
		return JITSupportGrant{}, ErrInvalid
	}
	if err := s.auth.AuthorizePlatform(ctx, actor, "dangerous:review"); err != nil {
		return JITSupportGrant{}, err
	}
	at, err := s.supportNow()
	if err != nil {
		return JITSupportGrant{}, err
	}
	grant, err := s.management.RevokeJITSupport(ctx, workspace, grantID, actor.UserID, at, reason)
	if err != nil {
		return JITSupportGrant{}, err
	}
	if grant.ID != grantID || grant.WorkspaceID != workspace || grant.RevokedAt.IsZero() {
		return JITSupportGrant{}, ErrUnavailable
	}
	return grant, nil
}

func (s *SupportAccessService) Runs(ctx context.Context, actor Actor, workspace string) ([]SupportRun, error) {
	if !validID(actor.UserID) || !validID(workspace) {
		return nil, ErrInvalid
	}
	if err := s.auth.AuthorizePlatform(ctx, actor, "support:request"); err != nil {
		return nil, err
	}
	at, err := s.supportNow()
	if err != nil {
		return nil, err
	}
	items, err := s.reader.ListSupportRuns(ctx, workspace, actor.UserID, at)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.WorkspaceID != workspace || !validID(item.ID) || item.Version < 1 || item.CreatedAt.IsZero() || item.UpdatedAt.Before(item.CreatedAt) {
			return nil, ErrUnavailable
		}
	}
	return items, nil
}
