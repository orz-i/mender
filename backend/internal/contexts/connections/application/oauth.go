package application

import (
	"context"
	"strings"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/connections/domain"
)

type OAuthRandom interface {
	Token(int) (string, error)
}

type OAuthProvider interface {
	ProviderID() string
	AuthorizationURL(state, verifier string) (string, error)
	Exchange(context.Context, string, string) (OAuthAccessToken, error)
}

type OAuthAccessToken struct {
	Value     []byte
	ExpiresAt time.Time
}

type OAuthCredentialAddress struct {
	ProviderID, ConnectionID, CredentialVersionRef string
	ConnectionRevision                             int64
}

type OAuthCredentialStore interface {
	Store(context.Context, OAuthCredentialAddress, []byte) error
	Delete(context.Context, OAuthCredentialAddress) error
}

type NewOAuthConnection struct {
	WorkspaceID, ConnectionID, ProviderID, CredentialVersionRef, SubjectID string
	Revision                                                               int64
	CreatedAt, ExpiresAt                                                   time.Time
}

type OAuthRepository interface {
	CreateOAuthConnection(context.Context, NewOAuthConnection) (domain.Summary, error)
}

type OAuthChallenge struct {
	ProviderID, WorkspaceID, UserID, State, Verifier, AuthorizationURL string
	ExpiresAt                                                          time.Time
}

type OAuthCompletion struct {
	Connection HumanConnection
}

type OAuthService struct {
	authorizer HumanAuthorizer
	repository OAuthRepository
	provider   OAuthProvider
	store      OAuthCredentialStore
	random     OAuthRandom
	clock      interface{ Now() time.Time }
	flowTTL    time.Duration
}

func NewOAuth(authorizer HumanAuthorizer, repository OAuthRepository, provider OAuthProvider, store OAuthCredentialStore, random OAuthRandom, clock interface{ Now() time.Time }, flowTTL time.Duration) (*OAuthService, error) {
	if authorizer == nil || repository == nil || provider == nil || store == nil || random == nil || clock == nil || flowTTL < time.Minute || flowTTL > 15*time.Minute || !validID(provider.ProviderID()) {
		return nil, ErrUnavailable
	}
	return &OAuthService{authorizer: authorizer, repository: repository, provider: provider, store: store, random: random, clock: clock, flowTTL: flowTTL}, nil
}

func (s *OAuthService) Begin(ctx context.Context, actor HumanActor, workspace string) (OAuthChallenge, error) {
	if err := ctx.Err(); err != nil {
		return OAuthChallenge{}, err
	}
	if !validID(workspace) || !validID(actor.UserID) {
		return OAuthChallenge{}, ErrForbidden
	}
	if err := s.authorizer.Authorize(ctx, actor, workspace, "connection:manage"); err != nil {
		return OAuthChallenge{}, err
	}
	state, err := s.random.Token(32)
	if err != nil {
		return OAuthChallenge{}, ErrUnavailable
	}
	verifier, err := s.random.Token(32)
	if err != nil {
		return OAuthChallenge{}, ErrUnavailable
	}
	if len(state) < 32 || len(state) > 128 || len(verifier) < 43 || len(verifier) > 128 {
		return OAuthChallenge{}, ErrUnavailable
	}
	url, err := s.provider.AuthorizationURL(state, verifier)
	if err != nil || !strings.HasPrefix(url, "https://") {
		return OAuthChallenge{}, ErrUnavailable
	}
	at := s.clock.Now().UTC().Truncate(time.Microsecond)
	return OAuthChallenge{ProviderID: s.provider.ProviderID(), WorkspaceID: workspace, UserID: actor.UserID, State: state, Verifier: verifier, AuthorizationURL: url, ExpiresAt: at.Add(s.flowTTL)}, nil
}

func safeAccessToken(raw []byte) bool {
	if len(raw) < 1 || len(raw) > 16<<10 {
		return false
	}
	for _, ch := range raw {
		if ch < 0x21 || ch > 0x7e {
			return false
		}
	}
	return true
}

func (s *OAuthService) Complete(ctx context.Context, actor HumanActor, challenge OAuthChallenge, state, code string) (OAuthCompletion, error) {
	if err := ctx.Err(); err != nil {
		return OAuthCompletion{}, err
	}
	now := s.clock.Now().UTC().Truncate(time.Microsecond)
	if !validID(actor.UserID) || !validID(challenge.WorkspaceID) || actor.UserID != challenge.UserID || challenge.ProviderID != s.provider.ProviderID() || challenge.State == "" || challenge.State != state || challenge.Verifier == "" || code == "" || challenge.ExpiresAt.IsZero() || !now.Before(challenge.ExpiresAt) {
		return OAuthCompletion{}, ErrForbidden
	}
	if err := s.authorizer.Authorize(ctx, actor, challenge.WorkspaceID, "connection:manage"); err != nil {
		return OAuthCompletion{}, err
	}
	token, err := s.provider.Exchange(ctx, code, challenge.Verifier)
	if err != nil || !safeAccessToken(token.Value) || token.ExpiresAt.IsZero() || !token.ExpiresAt.After(now.Add(30*time.Second)) || token.ExpiresAt.After(now.Add(24*time.Hour)) {
		return OAuthCompletion{}, ErrUnavailable
	}
	connectionSuffix, err := s.random.Token(16)
	if err != nil {
		return OAuthCompletion{}, ErrUnavailable
	}
	credentialSuffix, err := s.random.Token(16)
	if err != nil {
		return OAuthCompletion{}, ErrUnavailable
	}
	connectionID, credentialRef := "conn_"+connectionSuffix, "oauth_"+credentialSuffix
	if !validID(connectionID) || !validID(credentialRef) {
		return OAuthCompletion{}, ErrUnavailable
	}
	address := OAuthCredentialAddress{ProviderID: challenge.ProviderID, ConnectionID: connectionID, CredentialVersionRef: credentialRef, ConnectionRevision: 1}
	if err = s.store.Store(ctx, address, token.Value); err != nil {
		return OAuthCompletion{}, ErrUnavailable
	}
	created, err := s.repository.CreateOAuthConnection(ctx, NewOAuthConnection{
		WorkspaceID: challenge.WorkspaceID, ConnectionID: connectionID, ProviderID: challenge.ProviderID,
		CredentialVersionRef: credentialRef, SubjectID: actor.UserID, Revision: 1, CreatedAt: now, ExpiresAt: token.ExpiresAt.UTC().Truncate(time.Microsecond),
	})
	if err != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), address)
		return OAuthCompletion{}, ErrUnavailable
	}
	if created.WorkspaceID != challenge.WorkspaceID || created.ConnectionID != connectionID || created.ProviderID != challenge.ProviderID || created.State != "active" || created.Revision != 1 || created.Validate() != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), address)
		return OAuthCompletion{}, ErrUnavailable
	}
	return OAuthCompletion{Connection: projectHumanConnection(created)}, nil
}

func validID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}
