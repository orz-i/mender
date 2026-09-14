package bootstrap

import (
	"context"
	"errors"
	"time"

	connectionpg "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/postgres"
	connectionrandom "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/random"
	connectionapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

type OAuthRefreshRuntimeConfig struct {
	DatabaseURL string
	LeadTime    time.Duration
}

type oauthRefreshRunner interface {
	RefreshOne(context.Context, string) (connectionapp.OAuthRefreshReceipt, error)
}

type ReviewedOAuthRefreshRuntime struct{ service oauthRefreshRunner }

func (r *ReviewedOAuthRefreshRuntime) RefreshOne(ctx context.Context, workspace string) (connectionapp.OAuthRefreshReceipt, error) {
	if r == nil || r.service == nil {
		return connectionapp.OAuthRefreshReceipt{}, errors.New("OAuth refresh runtime is not safely configured")
	}
	return r.service.RefreshOne(ctx, workspace)
}

func BuildOAuthRefreshRuntime(ctx context.Context, c OAuthRefreshRuntimeConfig, provider connectionapp.OAuthRefreshProvider, store connectionapp.OAuthRefreshCredentialStore) (*ReviewedOAuthRefreshRuntime, func(), error) {
	if c.DatabaseURL == "" || provider == nil || store == nil || c.LeadTime < time.Minute || c.LeadTime > time.Hour {
		return nil, nil, errors.New("OAuth refresh runtime is not safely configured")
	}
	pool, err := database.Open(ctx, c.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*ReviewedOAuthRefreshRuntime, func(), error) {
		pool.Close()
		return nil, nil, err
	}
	if err = migrations.Verify(ctx, pool); err != nil {
		return fail(err)
	}
	if err = database.OAuthRefresherRole(ctx, pool); err != nil {
		return fail(err)
	}
	service, err := connectionapp.NewOAuthRefresh(connectionpg.New(pool), provider, store, connectionrandom.Generator{}, systemClock{}, c.LeadTime)
	if err != nil {
		return fail(err)
	}
	return &ReviewedOAuthRefreshRuntime{service: service}, pool.Close, nil
}
