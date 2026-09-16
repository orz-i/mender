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

func (r *Deployments) ProviderAcceptsNewWork(ctx context.Context, provider string) error {
	if r == nil || r.pool == nil || provider == "" {
		return application.ErrInvocationUnavailable
	}
	var allowed bool
	if err := r.pool.QueryRow(ctx, `SELECT supply.provider_accepts_new_work($1)`, provider).Scan(&allowed); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return application.ErrInvocationUnavailable
	}
	if !allowed {
		return application.ErrProviderQuarantined
	}
	return nil
}

func (r *Deployments) FindDeployment(ctx context.Context, revision string) (domain.Deployment, error) {
	if r == nil || r.pool == nil {
		return domain.Deployment{}, application.ErrInvocationUnavailable
	}
	var d domain.Deployment
	var timeoutMS int
	var authHeader, idempotencyHeader, statusEndpoint, statusMethod, cancelEndpoint, cancelMethod, inputEndpoint, inputMethod, mcpProtocol *string
	var mcpStateless *bool
	err := r.pool.QueryRow(ctx, `SELECT revision,provider_id,transport_kind,endpoint_url,http_method,status_endpoint_url,status_http_method,cancel_endpoint_url,cancel_http_method,input_endpoint_url,input_http_method,auth_mode,auth_header_name,idempotency_header,mcp_protocol_version,mcp_stateless,request_timeout_ms,max_request_bytes,max_response_bytes,state,created_at FROM supply.deployments WHERE revision=$1`, revision).Scan(&d.Revision, &d.ProviderID, &d.TransportKind, &d.EndpointURL, &d.HTTPMethod, &statusEndpoint, &statusMethod, &cancelEndpoint, &cancelMethod, &inputEndpoint, &inputMethod, &d.AuthMode, &authHeader, &idempotencyHeader, &mcpProtocol, &mcpStateless, &timeoutMS, &d.MaxRequestBytes, &d.MaxResponseBytes, &d.State, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Deployment{}, application.ErrInvocationForbidden
	}
	if err != nil {
		return domain.Deployment{}, application.ErrInvocationUnavailable
	}
	if authHeader != nil {
		d.AuthHeaderName = *authHeader
	}
	if idempotencyHeader != nil {
		d.IdempotencyHeader = *idempotencyHeader
	}
	if mcpProtocol != nil {
		d.MCPProtocolVersion = *mcpProtocol
	}
	if mcpStateless != nil {
		d.MCPStateless = *mcpStateless
	}
	if statusEndpoint != nil {
		d.StatusEndpointURL = *statusEndpoint
	}
	if statusMethod != nil {
		d.StatusHTTPMethod = *statusMethod
	}
	if cancelEndpoint != nil {
		d.CancelEndpointURL = *cancelEndpoint
	}
	if cancelMethod != nil {
		d.CancelHTTPMethod = *cancelMethod
	}
	if inputEndpoint != nil {
		d.InputEndpointURL = *inputEndpoint
	}
	if inputMethod != nil {
		d.InputHTTPMethod = *inputMethod
	}
	d.RequestTimeout = time.Duration(timeoutMS) * time.Millisecond
	if d.Validate() != nil {
		return domain.Deployment{}, application.ErrInvocationUnavailable
	}
	return d, nil
}

var _ application.DeploymentRepository = (*Deployments)(nil)
