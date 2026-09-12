package application

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/identity/domain"
)

type RunStartDelegationRepository interface {
	CreateRunStartDelegation(context.Context, domain.RunStartDelegation) error
	FindRunStartDelegationByDigest(context.Context, string) (domain.RunStartDelegation, error)
	FindRunStartDelegationByID(context.Context, string) (domain.RunStartDelegation, error)
	RevokeRunStartDelegation(context.Context, string, string, string, time.Time) error
}

func validStartIdempotencyKey(value string) bool {
	if len(value) < 8 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if ch < '!' || ch > '~' {
			return false
		}
	}
	return true
}

type RunStartConstraint struct {
	ToolsetVersionID, ToolID, ToolVersion, ToolVersionID string
	ConnectionID, Currency                               string
	MaxChargeMicro                                       int64
	IdempotencyKey                                       string
}

type IssuedRunStartDelegation struct {
	DelegationID string
	Token        string
	WorkspaceID  string
	UserID       string
	Constraint   RunStartConstraint
	ExpiresAt    time.Time
}

type RunStartDelegationPrincipal struct {
	DelegationID, WorkspaceID, UserID string
	Constraint                        RunStartConstraint
}

type RunStartDelegationService struct {
	repository RunStartDelegationRepository
	sessions   *HumanSessionService
	codec      TokenCodec
	clock      Clock
	ttl        time.Duration
}

func NewRunStartDelegationService(repository RunStartDelegationRepository, sessions *HumanSessionService, codec TokenCodec, clock Clock, ttl time.Duration) (*RunStartDelegationService, error) {
	if repository == nil || sessions == nil || codec == nil || clock == nil || ttl < time.Minute || ttl > 10*time.Minute {
		return nil, ErrUnavailable
	}
	return &RunStartDelegationService{repository: repository, sessions: sessions, codec: codec, clock: clock, ttl: ttl}, nil
}

func validStartConstraint(c RunStartConstraint) bool {
	if !domain.ValidID(c.ToolsetVersionID) || !domain.ValidID(c.ToolID) || !validRunVersion(c.ToolVersion) || !domain.ValidID(c.ToolVersionID) || !domain.ValidID(c.ConnectionID) || len(c.Currency) != 3 || c.MaxChargeMicro < 0 || !validStartIdempotencyKey(c.IdempotencyKey) {
		return false
	}
	for _, ch := range c.Currency {
		if ch < 'A' || ch > 'Z' {
			return false
		}
	}
	return true
}

func validRunVersion(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for i, ch := range value {
		if i == 0 && !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9') {
			return false
		}
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == ':' || ch == '+' || ch == '-') {
			return false
		}
	}
	return true
}

func startDelegationID(digest string) (string, error) {
	if len(digest) != 64 {
		return "", ErrUnavailable
	}
	return "rsd_" + digest[:32], nil
}

func constraintFromDomain(d domain.RunStartDelegation) RunStartConstraint {
	return RunStartConstraint{ToolsetVersionID: d.ToolsetVersionID, ToolID: d.ToolID, ToolVersion: d.ToolVersion, ToolVersionID: d.ToolVersionID, ConnectionID: d.ConnectionID, Currency: d.Currency, MaxChargeMicro: d.MaxChargeMicro, IdempotencyKey: d.IdempotencyKey}
}

func (s *RunStartDelegationService) Issue(ctx context.Context, principal HumanPrincipal, workspace string, constraint RunStartConstraint) (IssuedRunStartDelegation, error) {
	if err := ctx.Err(); err != nil {
		return IssuedRunStartDelegation{}, err
	}
	if !domain.ValidID(workspace) || !validStartConstraint(constraint) {
		return IssuedRunStartDelegation{}, ErrForbidden
	}
	if err := s.sessions.AuthorizeWorkspace(ctx, principal, workspace, "run:create"); err != nil {
		return IssuedRunStartDelegation{}, ErrForbidden
	}
	raw, digest, err := s.codec.Generate()
	if err != nil {
		return IssuedRunStartDelegation{}, ErrUnavailable
	}
	id, err := startDelegationID(digest)
	if err != nil {
		return IssuedRunStartDelegation{}, err
	}
	at := s.clock.Now().UTC().Truncate(time.Microsecond)
	d := domain.RunStartDelegation{ID: id, Digest: digest, WorkspaceID: workspace, UserID: principal.UserID, ToolsetVersionID: constraint.ToolsetVersionID, ToolID: constraint.ToolID, ToolVersion: constraint.ToolVersion, ToolVersionID: constraint.ToolVersionID, ConnectionID: constraint.ConnectionID, Currency: constraint.Currency, MaxChargeMicro: constraint.MaxChargeMicro, IdempotencyKey: constraint.IdempotencyKey, CreatedAt: at, ExpiresAt: at.Add(s.ttl)}
	if !d.Validate() {
		return IssuedRunStartDelegation{}, ErrUnavailable
	}
	if err = s.repository.CreateRunStartDelegation(ctx, d); err != nil {
		return IssuedRunStartDelegation{}, ErrUnavailable
	}
	return IssuedRunStartDelegation{DelegationID: id, Token: raw, WorkspaceID: workspace, UserID: principal.UserID, Constraint: constraint, ExpiresAt: d.ExpiresAt}, nil
}

func (s *RunStartDelegationService) Authenticate(ctx context.Context, raw string) (RunStartDelegationPrincipal, error) {
	digest, err := s.codec.Digest(raw)
	if err != nil {
		return RunStartDelegationPrincipal{}, ErrUnauthenticated
	}
	d, err := s.repository.FindRunStartDelegationByDigest(ctx, digest)
	if err != nil || !s.codec.EqualDigest(digest, d.Digest) || !d.AllowsCreate(s.clock.Now()) {
		return RunStartDelegationPrincipal{}, ErrUnauthenticated
	}
	return RunStartDelegationPrincipal{DelegationID: d.ID, WorkspaceID: d.WorkspaceID, UserID: d.UserID, Constraint: constraintFromDomain(d)}, nil
}

func (s *RunStartDelegationService) Authorize(ctx context.Context, principal RunStartDelegationPrincipal, workspace string, request RunStartConstraint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !domain.ValidID(principal.DelegationID) || !domain.ValidID(principal.UserID) || workspace != principal.WorkspaceID || !validStartConstraint(principal.Constraint) || !validStartConstraint(request) {
		return ErrForbidden
	}
	d, err := s.repository.FindRunStartDelegationByID(ctx, principal.DelegationID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrForbidden
		}
		return ErrUnavailable
	}
	stored := constraintFromDomain(d)
	if d.ID != principal.DelegationID || d.UserID != principal.UserID || d.WorkspaceID != workspace || !d.AllowsCreate(s.clock.Now()) || stored != principal.Constraint {
		return ErrForbidden
	}
	if request.ToolsetVersionID != stored.ToolsetVersionID || request.ToolID != stored.ToolID || request.ToolVersion != stored.ToolVersion || request.ToolVersionID != stored.ToolVersionID || request.ConnectionID != stored.ConnectionID || request.Currency != stored.Currency || request.IdempotencyKey != stored.IdempotencyKey || request.MaxChargeMicro > stored.MaxChargeMicro {
		return ErrForbidden
	}
	return nil
}

func (s *RunStartDelegationService) Revoke(ctx context.Context, principal HumanPrincipal, workspace, delegationID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !domain.ValidID(workspace) || !domain.ValidID(delegationID) || !domain.ValidID(principal.UserID) {
		return ErrForbidden
	}
	if err := s.sessions.AuthorizeWorkspace(ctx, principal, workspace, "workspace:read"); err != nil {
		return ErrForbidden
	}
	if err := s.repository.RevokeRunStartDelegation(ctx, delegationID, workspace, principal.UserID, s.clock.Now().UTC().Truncate(time.Microsecond)); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return ErrUnavailable
	}
	return nil
}
