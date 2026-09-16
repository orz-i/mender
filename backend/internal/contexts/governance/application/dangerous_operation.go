package application

import (
	"context"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/governance/domain"
)

type DangerousOperationApproval struct {
	WorkspaceID, ID, RequesterUserID, SubjectKind, SubjectID string
	Action, TargetKind, TargetID, TargetVersion              string
	ParametersJSON, ParametersSHA256, Currency               string
	AmountMicro                                              *int64
	Reason, State, ReviewerUserID, DecisionNote              string
	RequestedAt, ExpiresAt, ReviewedAt, ConsumedAt           time.Time
}

func validCommerceBusinessKey(value string) bool {
	if len(value) < 1 || len(value) > 200 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' || ch == '.' || ch == ':') {
			return false
		}
	}
	return true
}

func validCommerceCurrency(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, ch := range value {
		if ch < 'A' || ch > 'Z' {
			return false
		}
	}
	return true
}

func (s *DangerousOperationService) RequestCommerceApproval(ctx context.Context, actor Actor, workspace string, request CommerceApprovalRequest) (DangerousOperationApproval, error) {
	validAction := request.Action == domain.DangerousActionCommerceRefund || request.Action == domain.DangerousActionCommerceAdjustment
	validBasis := request.Action == domain.DangerousActionCommerceRefund && request.BasisKind == "usage_settlement" && request.Direction == "credit" ||
		request.Action == domain.DangerousActionCommerceAdjustment && (request.BasisKind == "run" || request.BasisKind == "incident" || request.BasisKind == "reconciliation") && (request.Direction == "debit" || request.Direction == "credit")
	if !validID(actor.UserID) || !validID(workspace) || !validID(request.BasisID) || !validAction || !validBasis || !validCommerceBusinessKey(request.BusinessKey) || request.AmountMicro <= 0 || !validCommerceCurrency(request.Currency) || len([]rune(request.Reason)) < 1 || len([]rune(request.Reason)) > 1000 || request.TTL < domain.MinDangerousApprovalTTL || request.TTL > domain.MaxDangerousApprovalTTL {
		return DangerousOperationApproval{}, ErrInvalid
	}
	if err := s.auth.AuthorizePlatform(ctx, actor, "platform:operate"); err != nil {
		return DangerousOperationApproval{}, err
	}
	id, err := s.ids.NewDangerousOperationID()
	if err != nil || !validID(id) {
		return DangerousOperationApproval{}, ErrUnavailable
	}
	at, err := s.dangerousNow()
	if err != nil {
		return DangerousOperationApproval{}, err
	}
	item, err := s.repository.RequestCommerceApproval(ctx, workspace, id, actor.UserID, request.Action, request.BusinessKey, request.BasisKind, request.BasisID, request.Direction, request.AmountMicro, request.Currency, request.Reason, at, at.Add(request.TTL))
	if err != nil {
		return DangerousOperationApproval{}, err
	}
	targetKind := domain.DangerousTargetBillingAdjustment
	if request.Action == domain.DangerousActionCommerceRefund {
		targetKind = domain.DangerousTargetBillingRefund
	}
	if item.ID != id || item.RequesterUserID != actor.UserID || item.Action != request.Action || item.TargetKind != targetKind || item.TargetID != request.BasisID || item.TargetVersion != request.BusinessKey || item.AmountMicro == nil || *item.AmountMicro != request.AmountMicro || item.Currency != request.Currency || !validDangerousProjection(item, workspace) {
		return DangerousOperationApproval{}, ErrUnavailable
	}
	return item, nil
}

type CommerceApprovalRequest struct {
	Action, BusinessKey, BasisKind, BasisID, Direction, Currency, Reason string
	AmountMicro                                                          int64
	TTL                                                                  time.Duration
}

type DangerousOperationRepository interface {
	ListDangerousOperations(context.Context, string, time.Time) ([]DangerousOperationApproval, error)
	GetDangerousOperation(context.Context, string, string) (DangerousOperationApproval, error)
	RequestReleaseEmergency(context.Context, string, string, string, string, string, time.Time, time.Time) (DangerousOperationApproval, error)
	RequestCommerceApproval(context.Context, string, string, string, string, string, string, string, string, int64, string, string, time.Time, time.Time) (DangerousOperationApproval, error)
	ApproveDangerousOperation(context.Context, string, string, string, time.Time, string) (DangerousOperationApproval, error)
	RejectDangerousOperation(context.Context, string, string, string, time.Time, string) (DangerousOperationApproval, error)
}

type DangerousOperationIDs interface{ NewDangerousOperationID() (string, error) }

type DangerousOperationAuthorizer interface {
	Authenticate(context.Context, string) (Actor, error)
	AuthenticateMutation(context.Context, string, string) (Actor, error)
	Authorize(context.Context, Actor, string, string) error
	AuthorizePlatform(context.Context, Actor, string) error
}

type DangerousOperationService struct {
	repository DangerousOperationRepository
	auth       DangerousOperationAuthorizer
	ids        DangerousOperationIDs
	clock      Clock
}

func NewDangerousOperationService(repository DangerousOperationRepository, auth DangerousOperationAuthorizer, ids DangerousOperationIDs, clock Clock) (*DangerousOperationService, error) {
	if repository == nil || auth == nil || ids == nil || clock == nil {
		return nil, ErrUnavailable
	}
	return &DangerousOperationService{repository: repository, auth: auth, ids: ids, clock: clock}, nil
}

func dangerousDomain(value DangerousOperationApproval) domain.DangerousOperationApproval {
	binding := domain.DangerousOperationBinding{
		WorkspaceID: value.WorkspaceID, SubjectKind: value.SubjectKind, SubjectID: value.SubjectID,
		Action: value.Action, TargetKind: value.TargetKind, TargetID: value.TargetID, TargetVersion: value.TargetVersion,
		ParametersJSON: value.ParametersJSON, ParametersSHA256: value.ParametersSHA256, Currency: value.Currency,
	}
	if value.AmountMicro != nil {
		binding.HasAmount = true
		binding.AmountMicro = *value.AmountMicro
	}
	return domain.DangerousOperationApproval{
		WorkspaceID: value.WorkspaceID, ID: value.ID, RequesterUserID: value.RequesterUserID, ReviewerUserID: value.ReviewerUserID,
		DecisionNote: value.DecisionNote, Reason: value.Reason, Binding: binding, State: value.State,
		RequestedAt: value.RequestedAt, ExpiresAt: value.ExpiresAt, ReviewedAt: value.ReviewedAt, ConsumedAt: value.ConsumedAt,
	}
}

func validDangerousProjection(value DangerousOperationApproval, workspace string) bool {
	return value.WorkspaceID == workspace && dangerousDomain(value).Valid()
}

func (s *DangerousOperationService) dangerousNow() (time.Time, error) {
	at := s.clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() || at.Year() > 9999 {
		return time.Time{}, ErrUnavailable
	}
	return at, nil
}

func (s *DangerousOperationService) List(ctx context.Context, actor Actor, workspace string) ([]DangerousOperationApproval, error) {
	if !validID(actor.UserID) || !validID(workspace) {
		return nil, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "release:manage"); err != nil {
		return nil, err
	}
	at, err := s.dangerousNow()
	if err != nil {
		return nil, err
	}
	items, err := s.repository.ListDangerousOperations(ctx, workspace, at)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if !validDangerousProjection(items[i], workspace) {
			return nil, ErrUnavailable
		}
		if (items[i].State == domain.DangerousApprovalPending || items[i].State == domain.DangerousApprovalApproved) && !at.Before(items[i].ExpiresAt) {
			items[i].State = domain.DangerousApprovalExpired
		}
	}
	return items, nil
}

func (s *DangerousOperationService) RequestReleaseEmergency(ctx context.Context, actor Actor, workspace, releaseID, reason string, ttl time.Duration) (DangerousOperationApproval, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(releaseID) || len([]rune(reason)) < 1 || len([]rune(reason)) > 1000 || ttl < domain.MinDangerousApprovalTTL || ttl > domain.MaxDangerousApprovalTTL {
		return DangerousOperationApproval{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "release:manage"); err != nil {
		return DangerousOperationApproval{}, err
	}
	id, err := s.ids.NewDangerousOperationID()
	if err != nil || !validID(id) {
		return DangerousOperationApproval{}, ErrUnavailable
	}
	at, err := s.dangerousNow()
	if err != nil {
		return DangerousOperationApproval{}, err
	}
	item, err := s.repository.RequestReleaseEmergency(ctx, workspace, id, actor.UserID, releaseID, reason, at, at.Add(ttl))
	if err != nil {
		return DangerousOperationApproval{}, err
	}
	if item.ID != id || item.RequesterUserID != actor.UserID || item.Action != domain.DangerousActionReleaseEmergencyDisable || item.TargetID != releaseID || !validDangerousProjection(item, workspace) {
		return DangerousOperationApproval{}, ErrUnavailable
	}
	return item, nil
}

func (s *DangerousOperationService) Approve(ctx context.Context, actor Actor, workspace, id, note string) (DangerousOperationApproval, error) {
	return s.decide(ctx, actor, workspace, id, note, true)
}

func (s *DangerousOperationService) Reject(ctx context.Context, actor Actor, workspace, id, note string) (DangerousOperationApproval, error) {
	return s.decide(ctx, actor, workspace, id, note, false)
}

func (s *DangerousOperationService) decide(ctx context.Context, actor Actor, workspace, id, note string, approve bool) (DangerousOperationApproval, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(id) || len([]rune(note)) > 1000 {
		return DangerousOperationApproval{}, ErrInvalid
	}
	current, err := s.repository.GetDangerousOperation(ctx, workspace, id)
	if err != nil {
		return DangerousOperationApproval{}, err
	}
	switch current.Action {
	case domain.DangerousActionReleaseEmergencyDisable:
		if err = s.auth.Authorize(ctx, actor, workspace, "release:manage"); err != nil {
			return DangerousOperationApproval{}, err
		}
	case domain.DangerousActionSupportWorkspaceRead, domain.DangerousActionCommerceRefund, domain.DangerousActionCommerceAdjustment:
		if err = s.auth.AuthorizePlatform(ctx, actor, "dangerous:review"); err != nil {
			return DangerousOperationApproval{}, err
		}
	default:
		return DangerousOperationApproval{}, ErrForbidden
	}
	at, err := s.dangerousNow()
	if err != nil {
		return DangerousOperationApproval{}, err
	}
	var item DangerousOperationApproval
	if approve {
		item, err = s.repository.ApproveDangerousOperation(ctx, workspace, id, actor.UserID, at, note)
	} else {
		item, err = s.repository.RejectDangerousOperation(ctx, workspace, id, actor.UserID, at, note)
	}
	if err != nil {
		return DangerousOperationApproval{}, err
	}
	if item.ID != id || !validDangerousProjection(item, workspace) {
		return DangerousOperationApproval{}, ErrUnavailable
	}
	return item, nil
}
