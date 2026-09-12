package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type MCPRuntime struct{ pool *pgxpool.Pool }

func NewMCPRuntime(pool *pgxpool.Pool) *MCPRuntime { return &MCPRuntime{pool: pool} }

func (r *MCPRuntime) ResolveMCPToolRoute(ctx context.Context, toolVersionID, deploymentRevision string) (domain.MCPToolRoute, error) {
	if r == nil || r.pool == nil {
		return domain.MCPToolRoute{}, application.ErrMCPRouteUnavailable
	}
	var route domain.MCPToolRoute
	err := r.pool.QueryRow(ctx, `SELECT tool_version_id,deployment_revision,upstream_tool_name,snapshot_sha256,state,created_at FROM supply.mcp_tool_routes WHERE tool_version_id=$1 AND deployment_revision=$2`, toolVersionID, deploymentRevision).Scan(&route.ToolVersionID, &route.DeploymentRevision, &route.UpstreamToolName, &route.SnapshotSHA256, &route.State, &route.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.MCPToolRoute{}, application.ErrInvocationForbidden
	}
	if err != nil || !route.Active() {
		return domain.MCPToolRoute{}, application.ErrMCPRouteUnavailable
	}
	return route, nil
}

func rollbackMCP(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func (r *MCPRuntime) scoped(ctx context.Context, workspace string) (pgx.Tx, error) {
	if r == nil || r.pool == nil || workspace == "" {
		return nil, application.ErrMCPResultUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrMCPResultUnavailable
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace); err != nil {
		rollbackMCP(tx)
		return nil, application.ErrMCPResultUnavailable
	}
	return tx, nil
}

func (r *MCPRuntime) SaveMCPCallResult(ctx context.Context, value application.MCPCallResult) error {
	if value.Validate() != nil {
		return application.ErrMCPResultUnavailable
	}
	tx, err := r.scoped(ctx, value.WorkspaceID)
	if err != nil {
		return err
	}
	defer rollbackMCP(tx)
	_, err = tx.Exec(ctx, `INSERT INTO supply.mcp_call_results(workspace_id,run_id,deployment_revision,submission_key,provider_id,provider_request_id,state,result_json,error_code,observed_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,CASE WHEN $8='' THEN NULL ELSE $8::jsonb END,NULLIF($9,''),$10)
 ON CONFLICT (workspace_id,run_id,submission_key) DO NOTHING`, value.WorkspaceID, value.RunID, value.DeploymentRevision, value.SubmissionKey, value.ProviderID, value.ProviderRequestID, value.State, value.ResultJSON, value.ErrorCode, value.ObservedAt)
	if err != nil {
		return application.ErrMCPResultUnavailable
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT deployment_revision=$4 AND provider_id=$5 AND provider_request_id=$6 AND state=$7
 AND result_json IS NOT DISTINCT FROM CASE WHEN $8='' THEN NULL ELSE $8::jsonb END
	 AND error_code IS NOT DISTINCT FROM NULLIF($9,'')
	 FROM supply.mcp_call_results WHERE workspace_id=$1 AND run_id=$2 AND submission_key=$3`, value.WorkspaceID, value.RunID, value.SubmissionKey, value.DeploymentRevision, value.ProviderID, value.ProviderRequestID, value.State, value.ResultJSON, value.ErrorCode).Scan(&same)
	if err != nil || !same {
		return application.ErrMCPResultUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ErrMCPResultUnavailable
	}
	return nil
}

func (r *MCPRuntime) FindMCPCallResult(ctx context.Context, workspace, run, providerRequestID string) (application.MCPCallResult, error) {
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return application.MCPCallResult{}, err
	}
	defer rollbackMCP(tx)
	var value application.MCPCallResult
	var resultJSON, errorCode *string
	err = tx.QueryRow(ctx, `SELECT workspace_id,run_id,deployment_revision,submission_key,provider_id,provider_request_id,state,result_json::text,error_code,observed_at FROM supply.mcp_call_results WHERE workspace_id=$1 AND run_id=$2 AND provider_request_id=$3`, workspace, run, providerRequestID).Scan(&value.WorkspaceID, &value.RunID, &value.DeploymentRevision, &value.SubmissionKey, &value.ProviderID, &value.ProviderRequestID, &value.State, &resultJSON, &errorCode, &value.ObservedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.MCPCallResult{}, supply.ErrProviderStatusNotFound
	}
	if err != nil {
		return application.MCPCallResult{}, application.ErrMCPResultUnavailable
	}
	if resultJSON != nil {
		value.ResultJSON = *resultJSON
	}
	if errorCode != nil {
		value.ErrorCode = *errorCode
	}
	if value.Validate() != nil {
		return application.MCPCallResult{}, application.ErrMCPResultUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return application.MCPCallResult{}, application.ErrMCPResultUnavailable
	}
	return value, nil
}

func (r *MCPRuntime) QueryStatus(ctx context.Context, query supply.StatusQuery) (supply.StatusObservation, error) {
	value, err := r.FindMCPCallResult(ctx, query.WorkspaceID, query.RunID, query.ProviderRequestID)
	if err != nil {
		return supply.StatusObservation{}, err
	}
	if value.ProviderID != query.ProviderID {
		return supply.StatusObservation{}, supply.ErrProviderStatusNotFound
	}
	state := supply.StatusSucceeded
	if value.State == "failed" {
		state = supply.StatusFailed
	}
	digest := sha256.Sum256([]byte(value.ProviderRequestID))
	return supply.StatusObservation{ObservationID: "mcp." + hex.EncodeToString(digest[:]), State: state, ResultJSON: value.ResultJSON, ErrorCode: value.ErrorCode, ObservedAt: value.ObservedAt}, nil
}

var _ application.MCPToolRouteRepository = (*MCPRuntime)(nil)
var _ application.MCPCallResultRepository = (*MCPRuntime)(nil)
var _ supply.ProviderStatusReader = (*MCPRuntime)(nil)
