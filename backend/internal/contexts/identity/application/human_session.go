package application

import (
	"context"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/identity/domain"
)

type VerifiedOIDCIdentity struct {
	Issuer, Subject string
}

type HumanIdentity struct {
	UserID, DisplayName string
	Disabled            bool
}

type HumanSessionRepository interface {
	ResolveOIDCIdentity(context.Context, string, string) (HumanIdentity, error)
	CreateBrowserSession(context.Context, domain.HumanSession) error
	FindBrowserSession(context.Context, string) (domain.HumanSession, error)
	RevokeBrowserSession(context.Context, string, time.Time) error
	ListWorkspaceMemberships(context.Context, string) ([]domain.WorkspaceMembership, error)
	FindWorkspaceMembership(context.Context, string, string) (domain.WorkspaceMembership, error)
}

type TokenCodec interface {
	Generate() (raw, digest string, err error)
	Digest(string) (string, error)
	EqualDigest(string, string) bool
	EqualRaw(string, string) bool
}

type OIDCClient interface {
	AuthorizationURL(state, nonce, verifier string) string
	Exchange(context.Context, string, string, string) (VerifiedOIDCIdentity, error)
}

type HumanPrincipal struct {
	UserID, SessionDigest, CSRFDigest string
}

type IssuedSession struct {
	SessionToken, CSRFToken string
	ExpiresAt               time.Time
	User                    HumanIdentity
}

type WorkspaceAccess struct {
	WorkspaceID string
	Role        domain.MembershipRole
}

type LoginChallenge struct {
	State, Nonce, Verifier, AuthorizationURL string
	ExpiresAt                                time.Time
}

type HumanSessionService struct {
	repository HumanSessionRepository
	codec      TokenCodec
	clock      Clock
	ttl        time.Duration
}

func NewHumanSessionService(repository HumanSessionRepository, codec TokenCodec, clock Clock, ttl time.Duration) (*HumanSessionService, error) {
	if repository == nil || codec == nil || clock == nil || ttl < 5*time.Minute || ttl > 24*time.Hour {
		return nil, ErrUnavailable
	}
	return &HumanSessionService{repository: repository, codec: codec, clock: clock, ttl: ttl}, nil
}

func (s *HumanSessionService) IssueVerified(ctx context.Context, verified VerifiedOIDCIdentity) (IssuedSession, error) {
	if err := ctx.Err(); err != nil {
		return IssuedSession{}, err
	}
	if verified.Issuer == "" || len(verified.Issuer) > 2048 || verified.Subject == "" || len(verified.Subject) > 512 {
		return IssuedSession{}, ErrUnauthenticated
	}
	user, err := s.repository.ResolveOIDCIdentity(ctx, verified.Issuer, verified.Subject)
	if err != nil || !domain.ValidID(user.UserID) || user.Disabled {
		return IssuedSession{}, ErrUnauthenticated
	}
	raw, digest, err := s.codec.Generate()
	if err != nil {
		return IssuedSession{}, ErrUnavailable
	}
	csrfRaw, csrfDigest, err := s.codec.Generate()
	if err != nil {
		return IssuedSession{}, ErrUnavailable
	}
	at := s.clock.Now().UTC().Truncate(time.Microsecond)
	session := domain.HumanSession{Digest: digest, UserID: user.UserID, CSRFDigest: csrfDigest, CreatedAt: at, ExpiresAt: at.Add(s.ttl)}
	if err = s.repository.CreateBrowserSession(ctx, session); err != nil {
		return IssuedSession{}, ErrUnavailable
	}
	return IssuedSession{SessionToken: raw, CSRFToken: csrfRaw, ExpiresAt: session.ExpiresAt, User: user}, nil
}

func (s *HumanSessionService) Authenticate(ctx context.Context, raw string) (HumanPrincipal, error) {
	digest, err := s.codec.Digest(raw)
	if err != nil {
		return HumanPrincipal{}, ErrUnauthenticated
	}
	session, err := s.repository.FindBrowserSession(ctx, digest)
	if err != nil || !session.ActiveAt(s.clock.Now()) || !s.codec.EqualDigest(digest, session.Digest) {
		return HumanPrincipal{}, ErrUnauthenticated
	}
	return HumanPrincipal{UserID: session.UserID, SessionDigest: session.Digest, CSRFDigest: session.CSRFDigest}, nil
}

func (s *HumanSessionService) VerifyCSRF(principal HumanPrincipal, raw string) error {
	digest, err := s.codec.Digest(raw)
	if err != nil || !s.codec.EqualDigest(digest, principal.CSRFDigest) {
		return ErrForbidden
	}
	return nil
}

func (s *HumanSessionService) Revoke(ctx context.Context, principal HumanPrincipal, csrf string) error {
	if err := s.VerifyCSRF(principal, csrf); err != nil {
		return err
	}
	return s.repository.RevokeBrowserSession(ctx, principal.SessionDigest, s.clock.Now().UTC().Truncate(time.Microsecond))
}

func (s *HumanSessionService) ListWorkspaces(ctx context.Context, principal HumanPrincipal) ([]WorkspaceAccess, error) {
	memberships, err := s.repository.ListWorkspaceMemberships(ctx, principal.UserID)
	if err != nil {
		return nil, ErrUnavailable
	}
	result := make([]WorkspaceAccess, 0, len(memberships))
	for _, membership := range memberships {
		if membership.UserID != principal.UserID || !membership.Active() {
			continue
		}
		result = append(result, WorkspaceAccess{WorkspaceID: membership.WorkspaceID, Role: membership.Role})
	}
	return result, nil
}

func (s *HumanSessionService) AuthorizeWorkspace(ctx context.Context, principal HumanPrincipal, workspace, action string) error {
	return s.AuthorizeUserWorkspace(ctx, principal.UserID, workspace, action)
}

func (s *HumanSessionService) AuthorizeUserWorkspace(ctx context.Context, userID, workspace, action string) error {
	if !domain.ValidID(userID) || !domain.ValidID(workspace) {
		return ErrForbidden
	}
	membership, err := s.repository.FindWorkspaceMembership(ctx, userID, workspace)
	if err != nil || membership.UserID != userID || membership.WorkspaceID != workspace || !membership.Allows(action) {
		return ErrForbidden
	}
	return nil
}

type LoginService struct {
	oidc     OIDCClient
	sessions *HumanSessionService
	codec    TokenCodec
	clock    Clock
}

func NewLoginService(oidc OIDCClient, sessions *HumanSessionService, codec TokenCodec, clock Clock) (*LoginService, error) {
	if oidc == nil || sessions == nil || codec == nil || clock == nil {
		return nil, ErrUnavailable
	}
	return &LoginService{oidc: oidc, sessions: sessions, codec: codec, clock: clock}, nil
}

func (s *LoginService) Begin(ctx context.Context) (LoginChallenge, error) {
	if err := ctx.Err(); err != nil {
		return LoginChallenge{}, err
	}
	state, _, e1 := s.codec.Generate()
	nonce, _, e2 := s.codec.Generate()
	verifier, _, e3 := s.codec.Generate()
	if e1 != nil || e2 != nil || e3 != nil {
		return LoginChallenge{}, ErrUnavailable
	}
	expires := s.clock.Now().UTC().Add(10 * time.Minute)
	return LoginChallenge{State: state, Nonce: nonce, Verifier: verifier, AuthorizationURL: s.oidc.AuthorizationURL(state, nonce, verifier), ExpiresAt: expires}, nil
}

func (s *LoginService) Complete(ctx context.Context, challenge LoginChallenge, state, code string) (IssuedSession, error) {
	if challenge.State == "" || challenge.Nonce == "" || challenge.Verifier == "" || code == "" || !s.codec.EqualRaw(state, challenge.State) || !s.clock.Now().Before(challenge.ExpiresAt) {
		return IssuedSession{}, ErrUnauthenticated
	}
	verified, err := s.oidc.Exchange(ctx, code, challenge.Verifier, challenge.Nonce)
	if err != nil {
		return IssuedSession{}, ErrUnauthenticated
	}
	return s.sessions.IssueVerified(ctx, verified)
}
