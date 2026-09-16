package postgres

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

func (r *DangerousOperationRepository) RequestSupportJIT(ctx context.Context, workspace, id, requester string, scopes []string, ttl time.Duration, reason string, at, expires time.Time) (application.DangerousOperationApproval, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.DangerousOperationApproval{}, err
	}
	defer rollback(tx)
	ordered := append([]string(nil), scopes...)
	sort.Strings(ordered)
	if _, err = tx.Exec(ctx, `SELECT governance.request_support_jit_approval($1,$2,$3,$4,$5,$6,$7,$8)`, workspace, id, requester, ordered, int64(ttl/time.Second), reason, at, expires); err != nil {
		return application.DangerousOperationApproval{}, dangerousError(err)
	}
	item, err := r.find(ctx, tx, workspace, id)
	if err != nil {
		return item, err
	}
	if err = tx.Commit(ctx); err != nil {
		return item, application.ErrUnavailable
	}
	return item, nil
}

func scanJITGrant(row pgx.Row) (application.JITSupportGrant, error) {
	var grant application.JITSupportGrant
	var revoked *time.Time
	err := row.Scan(&grant.WorkspaceID, &grant.ID, &grant.ApprovalID, &grant.UserID, &grant.Scopes, &grant.Reason, &grant.CreatedAt, &grant.ExpiresAt, &revoked)
	if revoked != nil {
		grant.RevokedAt = *revoked
	}
	return grant, dangerousError(err)
}

func (r *DangerousOperationRepository) jitGrant(ctx context.Context, tx pgx.Tx, workspace, id string) (application.JITSupportGrant, error) {
	return scanJITGrant(tx.QueryRow(ctx, `SELECT workspace_id,id,approval_id,user_id,scopes,reason,created_at,expires_at,revoked_at FROM governance.jit_support_grants WHERE workspace_id=$1 AND id=$2`, workspace, id))
}

func (r *DangerousOperationRepository) ActivateJITSupport(ctx context.Context, workspace, approvalID, grantID, userID string, at time.Time) (application.JITSupportGrant, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.JITSupportGrant{}, err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT governance.activate_jit_support($1,$2,$3,$4,$5)`, workspace, approvalID, grantID, userID, at); err != nil {
		return application.JITSupportGrant{}, dangerousError(err)
	}
	grant, err := r.jitGrant(ctx, tx, workspace, grantID)
	if err != nil {
		return grant, err
	}
	if err = tx.Commit(ctx); err != nil {
		return grant, application.ErrUnavailable
	}
	return grant, nil
}

func (r *DangerousOperationRepository) RevokeJITSupport(ctx context.Context, workspace, grantID, actor string, at time.Time, reason string) (application.JITSupportGrant, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.JITSupportGrant{}, err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT governance.revoke_jit_support($1,$2,$3,$4,$5)`, workspace, grantID, actor, at, reason); err != nil {
		return application.JITSupportGrant{}, dangerousError(err)
	}
	grant, err := r.jitGrant(ctx, tx, workspace, grantID)
	if err != nil {
		return grant, err
	}
	if err = tx.Commit(ctx); err != nil {
		return grant, application.ErrUnavailable
	}
	return grant, nil
}

type SupportReadRepository struct{ pool *pgxpool.Pool }

func NewSupportRead(pool *pgxpool.Pool) *SupportReadRepository {
	return &SupportReadRepository{pool: pool}
}

func (r *SupportReadRepository) ListSupportRuns(ctx context.Context, workspace, userID string, at time.Time) ([]application.SupportRun, error) {
	if r == nil || r.pool == nil {
		return nil, application.ErrUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrUnavailable
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace); err != nil {
		return nil, application.ErrUnavailable
	}
	var grantID *string
	if err = tx.QueryRow(ctx, `SELECT governance.authorize_jit_support($1,$2,'run:read',$3)`, workspace, userID, at).Scan(&grantID); err != nil {
		return nil, dangerousError(err)
	}
	if grantID == nil || *grantID == "" {
		return nil, application.ErrForbidden
	}
	rows, err := tx.Query(ctx, `SELECT workspace_id,id,state,version,created_at,updated_at FROM execution.runs WHERE workspace_id=$1 ORDER BY updated_at DESC,id DESC LIMIT 100`, workspace)
	if err != nil {
		return nil, dangerousError(err)
	}
	defer rows.Close()
	items := []application.SupportRun{}
	for rows.Next() {
		var item application.SupportRun
		if err = rows.Scan(&item.WorkspaceID, &item.ID, &item.State, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, application.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, application.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, application.ErrUnavailable
	}
	return items, nil
}

var _ application.SupportManagementRepository = (*DangerousOperationRepository)(nil)
var _ application.SupportReadRepository = (*SupportReadRepository)(nil)
