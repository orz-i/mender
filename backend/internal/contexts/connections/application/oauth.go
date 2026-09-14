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

func normalizeOAuthScopes(values []string, max int) ([]string, error) {
	if len(values) < 1 || len(values) > max {
		return nil, ErrUnavailable
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 128 || strings.ContainsAny(value, " \t\r\n") || seen[value] {
			return nil, ErrUnavailable
		}
		seen[value] = true
		result = append(result, value)
	}
	return result, nil
}

func containsScopes(granted, required []string) bool {
	set := make(map[string]bool, len(granted))
	for _, scope := range granted {
		set[scope] = true
	}
	for _, scope := range required {
		if !set[scope] {
			return false
		}
	}
	return true
}

type OAuthProvider interface {
	ProviderID() string
	AuthorizationURL(state, verifier string) (string, error)
	Exchange(context.Context, string, string) (OAuthAccessToken, error)
}

type OAuthAccessToken struct {
	Value        []byte
	RefreshValue []byte
	ExpiresAt    time.Time
	Scopes       []string
}

type OAuthCredentialAddress struct {
	ProviderID, ConnectionID, CredentialVersionRef string
	ConnectionRevision                             int64
}

type OAuthCredentialStore interface {
	Store(context.Context, OAuthCredentialAddress, []byte) error
	Delete(context.Context, OAuthCredentialAddress) error
}

type OAuthRefreshCredentialStore interface {
	OAuthCredentialStore
	Load(context.Context, OAuthCredentialAddress) ([]byte, error)
}

type NewOAuthConnection struct {
	WorkspaceID, ConnectionID, ProviderID, CredentialVersionRef, SubjectID string
	Revision                                                               int64
	CreatedAt, ExpiresAt                                                   time.Time
	RefreshCredentialRef                                                   string
	RefreshSecretRevision                                                  int64
	RequiredScopes, GrantedScopes                                          []string
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
	authorizer     HumanAuthorizer
	repository     OAuthRepository
	provider       OAuthProvider
	store          OAuthCredentialStore
	refresh        OAuthRefreshCredentialStore
	random         OAuthRandom
	clock          interface{ Now() time.Time }
	flowTTL        time.Duration
	requiredScopes []string
}

func NewOAuth(authorizer HumanAuthorizer, repository OAuthRepository, provider OAuthProvider, store OAuthCredentialStore, random OAuthRandom, clock interface{ Now() time.Time }, flowTTL time.Duration) (*OAuthService, error) {
	if authorizer == nil || repository == nil || provider == nil || store == nil || random == nil || clock == nil || flowTTL < time.Minute || flowTTL > 15*time.Minute || !validID(provider.ProviderID()) {
		return nil, ErrUnavailable
	}
	return &OAuthService{authorizer: authorizer, repository: repository, provider: provider, store: store, random: random, clock: clock, flowTTL: flowTTL}, nil
}

func NewRefreshableOAuth(authorizer HumanAuthorizer, repository OAuthRepository, provider OAuthProvider, store OAuthRefreshCredentialStore, random OAuthRandom, clock interface{ Now() time.Time }, flowTTL time.Duration, requiredScopes []string) (*OAuthService, error) {
	service, err := NewOAuth(authorizer, repository, provider, store, random, clock, flowTTL)
	if err != nil || store == nil {
		return nil, ErrUnavailable
	}
	scopes, err := normalizeOAuthScopes(requiredScopes, 16)
	if err != nil || len(scopes) == 0 {
		return nil, ErrUnavailable
	}
	service.refresh = store
	service.requiredScopes = scopes
	return service, nil
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
	var grantedScopes []string
	if s.refresh != nil {
		if !safeAccessToken(token.RefreshValue) {
			return OAuthCompletion{}, ErrUnavailable
		}
		if len(token.Scopes) == 0 {
			grantedScopes = append([]string(nil), s.requiredScopes...)
		} else {
			grantedScopes, err = normalizeOAuthScopes(token.Scopes, 32)
			if err != nil || !containsScopes(grantedScopes, s.requiredScopes) {
				return OAuthCompletion{}, ErrUnavailable
			}
		}
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
	refreshRef := ""
	if s.refresh != nil {
		refreshSuffix, randomErr := s.random.Token(16)
		if randomErr != nil {
			return OAuthCompletion{}, ErrUnavailable
		}
		refreshRef = "oauth_refresh_" + refreshSuffix
		if !validID(refreshRef) {
			return OAuthCompletion{}, ErrUnavailable
		}
	}
	address := OAuthCredentialAddress{ProviderID: challenge.ProviderID, ConnectionID: connectionID, CredentialVersionRef: credentialRef, ConnectionRevision: 1}
	if err = s.store.Store(ctx, address, token.Value); err != nil {
		return OAuthCompletion{}, ErrUnavailable
	}
	refreshAddress := OAuthCredentialAddress{}
	if s.refresh != nil {
		refreshAddress = OAuthCredentialAddress{ProviderID: challenge.ProviderID, ConnectionID: connectionID, CredentialVersionRef: refreshRef, ConnectionRevision: 1}
		if err = s.refresh.Store(ctx, refreshAddress, token.RefreshValue); err != nil {
			_ = s.store.Delete(context.WithoutCancel(ctx), address)
			return OAuthCompletion{}, ErrUnavailable
		}
	}
	created, err := s.repository.CreateOAuthConnection(ctx, NewOAuthConnection{
		WorkspaceID: challenge.WorkspaceID, ConnectionID: connectionID, ProviderID: challenge.ProviderID,
		CredentialVersionRef: credentialRef, SubjectID: actor.UserID, Revision: 1, CreatedAt: now, ExpiresAt: token.ExpiresAt.UTC().Truncate(time.Microsecond),
		RefreshCredentialRef: refreshRef, RefreshSecretRevision: 1, RequiredScopes: append([]string(nil), s.requiredScopes...), GrantedScopes: append([]string(nil), grantedScopes...),
	})
	if err != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), address)
		if s.refresh != nil {
			_ = s.refresh.Delete(context.WithoutCancel(ctx), refreshAddress)
		}
		return OAuthCompletion{}, ErrUnavailable
	}
	if created.WorkspaceID != challenge.WorkspaceID || created.ConnectionID != connectionID || created.ProviderID != challenge.ProviderID || created.State != "active" || created.Revision != 1 || created.Validate() != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), address)
		if s.refresh != nil {
			_ = s.refresh.Delete(context.WithoutCancel(ctx), refreshAddress)
		}
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
