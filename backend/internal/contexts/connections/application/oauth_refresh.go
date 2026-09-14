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
