package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/identity/domain"
)

type RunDelegationRepository interface {
	CreateRunDelegation(context.Context, domain.RunDelegation) error
	FindRunDelegationByDigest(context.Context, string) (domain.RunDelegation, error)
	FindRunDelegationByID(context.Context, string) (domain.RunDelegation, error)
	RevokeRunDelegation(context.Context, string, string, string, time.Time) error
}

type IssuedRunDelegation struct {
	DelegationID string
	Token        string
	WorkspaceID  string
	UserID       string
	Scopes       []string
	ExpiresAt    time.Time
}

type RunDelegationPrincipal struct {
	DelegationID, WorkspaceID, UserID string
}

type RunDelegationService struct {
	repository RunDelegationRepository
	sessions   *HumanSessionService
	codec      TokenCodec
	clock      Clock
	ttl        time.Duration
}

func NewRunDelegationService(repository RunDelegationRepository, sessions *HumanSessionService, codec TokenCodec, clock Clock, ttl time.Duration) (*RunDelegationService, error) {
	if repository == nil || sessions == nil || codec == nil || clock == nil || ttl < time.Minute || ttl > 30*time.Minute {
		return nil, ErrUnavailable
	}
	return &RunDelegationService{repository: repository, sessions: sessions, codec: codec, clock: clock, ttl: ttl}, nil
}

func normalizeDelegationScopes(scopes []string) ([]string, error) {
	if len(scopes) < 1 || len(scopes) > 3 {
		return nil, ErrForbidden
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(scopes))
	for _, raw := range scopes {
		scope := strings.TrimSpace(raw)
		if scope != "run:read" && scope != "run:cancel" && scope != "run:input" || seen[scope] {
			return nil, ErrForbidden
		}
		seen[scope] = true
		result = append(result, scope)
	}
	return result, nil
}

func delegationID(digest string) (string, error) {
	if len(digest) != 64 {
		return "", ErrUnavailable
	}
	return "rd_" + digest[:32], nil
}

func (s *RunDelegationService) Issue(ctx context.Context, principal HumanPrincipal, workspace string, scopes []string) (IssuedRunDelegation, error) {
	if err := ctx.Err(); err != nil {
		return IssuedRunDelegation{}, err
	}
	normalized, err := normalizeDelegationScopes(scopes)
	if err != nil {
		return IssuedRunDelegation{}, err
	}
	for _, scope := range normalized {
		if err = s.sessions.AuthorizeWorkspace(ctx, principal, workspace, scope); err != nil {
			return IssuedRunDelegation{}, ErrForbidden
		}
	}
	raw, digest, err := s.codec.Generate()
	if err != nil {
		return IssuedRunDelegation{}, ErrUnavailable
	}
	id, err := delegationID(digest)
	if err != nil {
		return IssuedRunDelegation{}, err
	}
	at := s.clock.Now().UTC().Truncate(time.Microsecond)
	delegation := domain.RunDelegation{ID: id, Digest: digest, WorkspaceID: workspace, UserID: principal.UserID, Scopes: append([]string(nil), normalized...), CreatedAt: at, ExpiresAt: at.Add(s.ttl)}
	if !delegation.Validate() {
		return IssuedRunDelegation{}, ErrUnavailable
	}
	if err = s.repository.CreateRunDelegation(ctx, delegation); err != nil {
		return IssuedRunDelegation{}, ErrUnavailable
	}
	return IssuedRunDelegation{DelegationID: id, Token: raw, WorkspaceID: workspace, UserID: principal.UserID, Scopes: append([]string(nil), normalized...), ExpiresAt: delegation.ExpiresAt}, nil
}

func (s *RunDelegationService) Authenticate(ctx context.Context, raw string) (RunDelegationPrincipal, error) {
	digest, err := s.codec.Digest(raw)
	if err != nil {
		return RunDelegationPrincipal{}, ErrUnauthenticated
	}
	delegation, err := s.repository.FindRunDelegationByDigest(ctx, digest)
	if err != nil || !s.codec.EqualDigest(digest, delegation.Digest) || !delegation.ActiveAt(s.clock.Now()) {
		return RunDelegationPrincipal{}, ErrUnauthenticated
	}
	return RunDelegationPrincipal{DelegationID: delegation.ID, WorkspaceID: delegation.WorkspaceID, UserID: delegation.UserID}, nil
}

func (s *RunDelegationService) Authorize(ctx context.Context, principal RunDelegationPrincipal, workspace, action string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !domain.ValidID(principal.DelegationID) || !domain.ValidID(principal.UserID) || !domain.ValidID(workspace) || workspace != principal.WorkspaceID || action != "run:read" && action != "run:cancel" && action != "run:input" {
		return ErrForbidden
	}
	delegation, err := s.repository.FindRunDelegationByID(ctx, principal.DelegationID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrForbidden
		}
		return ErrUnavailable
	}
	if delegation.ID != principal.DelegationID || delegation.UserID != principal.UserID || delegation.WorkspaceID != workspace || !delegation.Allows(action, s.clock.Now()) {
		return ErrForbidden
	}
	return nil
}

func (s *RunDelegationService) Revoke(ctx context.Context, principal HumanPrincipal, workspace, delegationID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !domain.ValidID(workspace) || !domain.ValidID(delegationID) || !domain.ValidID(principal.UserID) {
		return ErrForbidden
	}
	if err := s.sessions.AuthorizeWorkspace(ctx, principal, workspace, "workspace:read"); err != nil {
		return ErrForbidden
	}
	if err := s.repository.RevokeRunDelegation(ctx, delegationID, workspace, principal.UserID, s.clock.Now().UTC().Truncate(time.Microsecond)); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return ErrUnavailable
	}
	return nil
}
