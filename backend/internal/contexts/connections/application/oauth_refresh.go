package application

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/connections/domain"
)

var (
	ErrNoOAuthRefreshCandidate = errors.New("no oauth refresh candidate")
	ErrOAuthRefreshRejected    = errors.New("oauth refresh rejected")
)

type OAuthRefreshTarget struct {
	WorkspaceID, ConnectionID, ProviderID     string
	AccessCredentialRef, RefreshCredentialRef string
	ConnectionRevision, RefreshSecretRevision int64
	ExpiresAt                                 time.Time
	RequiredScopes, GrantedScopes             []string
}

type OAuthRefreshCommit struct {
	Target                 OAuthRefreshTarget
	AccessCredentialRef    string
	RefreshCredentialRef   string
	RefreshSecretRevision  int64
	GrantedScopes          []string
	ExpiresAt, RefreshedAt time.Time
}

type OAuthRefreshRepository interface {
	FindOAuthRefreshTarget(context.Context, string, string, time.Time) (OAuthRefreshTarget, error)
	CommitOAuthRefresh(context.Context, OAuthRefreshCommit) (domain.Summary, bool, error)
	FailOAuthRefresh(context.Context, OAuthRefreshTarget, string, time.Time) (domain.Summary, bool, error)
}

type OAuthRefreshProvider interface {
	ProviderID() string
	Refresh(context.Context, []byte) (OAuthAccessToken, error)
}

type OAuthRefreshReceipt struct {
	ConnectionID string
	Revision     int64
	Outcome      string
	ExpiresAt    time.Time
}

type OAuthRefreshService struct {
	repository OAuthRefreshRepository
	provider   OAuthRefreshProvider
	store      OAuthRefreshCredentialStore
	random     OAuthRandom
	clock      interface{ Now() time.Time }
	leadTime   time.Duration
}

func NewOAuthRefresh(repository OAuthRefreshRepository, provider OAuthRefreshProvider, store OAuthRefreshCredentialStore, random OAuthRandom, clock interface{ Now() time.Time }, leadTime time.Duration) (*OAuthRefreshService, error) {
	if repository == nil || provider == nil || store == nil || random == nil || clock == nil || !validID(provider.ProviderID()) || leadTime < time.Minute || leadTime > time.Hour {
		return nil, ErrUnavailable
	}
	return &OAuthRefreshService{repository: repository, provider: provider, store: store, random: random, clock: clock, leadTime: leadTime}, nil
}

func refreshAddress(target OAuthRefreshTarget) OAuthCredentialAddress {
	return OAuthCredentialAddress{ProviderID: target.ProviderID, ConnectionID: target.ConnectionID, CredentialVersionRef: target.RefreshCredentialRef, ConnectionRevision: target.RefreshSecretRevision}
}

func accessAddress(target OAuthRefreshTarget) OAuthCredentialAddress {
	return OAuthCredentialAddress{ProviderID: target.ProviderID, ConnectionID: target.ConnectionID, CredentialVersionRef: target.AccessCredentialRef, ConnectionRevision: target.ConnectionRevision}
}

func (s *OAuthRefreshService) fail(ctx context.Context, target OAuthRefreshTarget, code string, now time.Time) (OAuthRefreshReceipt, error) {
	item, committed, err := s.repository.FailOAuthRefresh(ctx, target, code, now)
	if err != nil {
		return OAuthRefreshReceipt{}, err
	}
	if !committed {
		return OAuthRefreshReceipt{}, ErrNoOAuthRefreshCandidate
	}
	return OAuthRefreshReceipt{ConnectionID: item.ConnectionID, Revision: item.Revision, Outcome: code, ExpiresAt: item.ExpiresAt}, nil
}

func (s *OAuthRefreshService) RefreshOne(ctx context.Context, workspace string) (OAuthRefreshReceipt, error) {
	if err := ctx.Err(); err != nil {
		return OAuthRefreshReceipt{}, err
	}
	if !validID(workspace) {
		return OAuthRefreshReceipt{}, ErrUnavailable
	}
	now := s.clock.Now().UTC().Truncate(time.Microsecond)
	if now.IsZero() {
		return OAuthRefreshReceipt{}, ErrUnavailable
	}
	target, err := s.repository.FindOAuthRefreshTarget(ctx, workspace, s.provider.ProviderID(), now.Add(s.leadTime))
	if err != nil {
		return OAuthRefreshReceipt{}, err
	}
	refreshSecret, err := s.store.Load(ctx, refreshAddress(target))
	if err != nil {
		return OAuthRefreshReceipt{}, err
	}
	token, err := s.provider.Refresh(ctx, refreshSecret)
	for i := range refreshSecret {
		refreshSecret[i] = 0
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return OAuthRefreshReceipt{}, err
		}
		if errors.Is(err, ErrOAuthRefreshRejected) {
			return s.fail(ctx, target, "invalid_grant", now)
		}
		return OAuthRefreshReceipt{}, ErrUnavailable
	}
	if !safeAccessToken(token.Value) || token.ExpiresAt.IsZero() || !token.ExpiresAt.After(now.Add(30*time.Second)) || token.ExpiresAt.After(now.Add(24*time.Hour)) {
		return s.fail(ctx, target, "invalid_token", now)
	}
	granted := append([]string(nil), target.GrantedScopes...)
	if len(token.Scopes) > 0 {
		granted, err = normalizeOAuthScopes(token.Scopes, 32)
		if err != nil {
			return s.fail(ctx, target, "scope_reduced", now)
		}
	}
	if !containsScopes(granted, target.RequiredScopes) {
		return s.fail(ctx, target, "scope_reduced", now)
	}
	accessSuffix, err := s.random.Token(16)
	if err != nil {
		return OAuthRefreshReceipt{}, ErrUnavailable
	}
	accessRef := "oauth_" + accessSuffix
	if !validID(accessRef) {
		return OAuthRefreshReceipt{}, ErrUnavailable
	}
	newRevision := target.ConnectionRevision + 1
	newAccess := OAuthCredentialAddress{ProviderID: target.ProviderID, ConnectionID: target.ConnectionID, CredentialVersionRef: accessRef, ConnectionRevision: newRevision}
	if err = s.store.Store(ctx, newAccess, token.Value); err != nil {
		return OAuthRefreshReceipt{}, ErrUnavailable
	}
	cleanupNew := true
	defer func() {
		if cleanupNew {
			_ = s.store.Delete(context.WithoutCancel(ctx), newAccess)
		}
	}()

	refreshRef := target.RefreshCredentialRef
	refreshRevision := target.RefreshSecretRevision
	rotated := OAuthCredentialAddress{}
	if len(token.RefreshValue) > 0 {
		if !safeAccessToken(token.RefreshValue) {
			return s.fail(ctx, target, "invalid_token", now)
		}
		refreshSuffix, randomErr := s.random.Token(16)
		if randomErr != nil {
			return OAuthRefreshReceipt{}, ErrUnavailable
		}
		refreshRef = "oauth_refresh_" + refreshSuffix
		refreshRevision++
		if !validID(refreshRef) {
			return OAuthRefreshReceipt{}, ErrUnavailable
		}
		rotated = OAuthCredentialAddress{ProviderID: target.ProviderID, ConnectionID: target.ConnectionID, CredentialVersionRef: refreshRef, ConnectionRevision: refreshRevision}
		if err = s.store.Store(ctx, rotated, token.RefreshValue); err != nil {
			return OAuthRefreshReceipt{}, ErrUnavailable
		}
		defer func() {
			if cleanupNew {
				_ = s.store.Delete(context.WithoutCancel(ctx), rotated)
			}
		}()
	}

	item, committed, err := s.repository.CommitOAuthRefresh(ctx, OAuthRefreshCommit{
		Target: target, AccessCredentialRef: accessRef, RefreshCredentialRef: refreshRef,
		RefreshSecretRevision: refreshRevision, GrantedScopes: granted,
		ExpiresAt: token.ExpiresAt.UTC().Truncate(time.Microsecond), RefreshedAt: now,
	})
	if err != nil {
		return OAuthRefreshReceipt{}, err
	}
	if !committed {
		return OAuthRefreshReceipt{}, ErrNoOAuthRefreshCandidate
	}
	cleanupNew = false
	_ = s.store.Delete(context.WithoutCancel(ctx), accessAddress(target))
	if rotated.CredentialVersionRef != "" {
		_ = s.store.Delete(context.WithoutCancel(ctx), refreshAddress(target))
	}
	return OAuthRefreshReceipt{ConnectionID: item.ConnectionID, Revision: item.Revision, Outcome: "refreshed", ExpiresAt: item.ExpiresAt}, nil
}
