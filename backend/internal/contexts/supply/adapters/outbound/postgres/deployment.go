package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
)

type Deployments struct{ pool *pgxpool.Pool }

func NewDeployments(pool *pgxpool.Pool) *Deployments { return &Deployments{pool: pool} }

func (r *Deployments) FindDeployment(ctx context.Context, revision string) (domain.Deployment, error) {
	if r == nil || r.pool == nil {
		return domain.Deployment{}, application.ErrInvocationUnavailable
	}
	var d domain.Deployment
	var timeoutMS int
	var authHeader *string
	err := r.pool.QueryRow(ctx, `SELECT revision,provider_id,transport_kind,endpoint_url,http_method,auth_mode,auth_header_name,idempotency_header,request_timeout_ms,max_request_bytes,max_response_bytes,state,created_at FROM supply.deployments WHERE revision=$1`, revision).Scan(&d.Revision, &d.ProviderID, &d.TransportKind, &d.EndpointURL, &d.HTTPMethod, &d.AuthMode, &authHeader, &d.IdempotencyHeader, &timeoutMS, &d.MaxRequestBytes, &d.MaxResponseBytes, &d.State, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Deployment{}, application.ErrInvocationForbidden
	}
	if err != nil {
		return domain.Deployment{}, application.ErrInvocationUnavailable
	}
	if authHeader != nil {
		d.AuthHeaderName = *authHeader
	}
	d.RequestTimeout = time.Duration(timeoutMS) * time.Millisecond
	if d.Validate() != nil {
		return domain.Deployment{}, application.ErrInvocationUnavailable
	}
	return d, nil
}

var _ application.DeploymentRepository = (*Deployments)(nil)
