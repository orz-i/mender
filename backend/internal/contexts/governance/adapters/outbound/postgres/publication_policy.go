package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

type PublicationPolicyRepository struct{ pool *pgxpool.Pool }

func NewPublicationPolicy(pool *pgxpool.Pool) *PublicationPolicyRepository {
	return &PublicationPolicyRepository{pool: pool}
}

func (r *PublicationPolicyRepository) begin(ctx context.Context, workspace string) (pgx.Tx, error) {
	if r == nil || r.pool == nil {
		return nil, application.ErrUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrUnavailable
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", workspace); err != nil {
		rollback(tx)
		return nil, application.ErrUnavailable
	}
	return tx, nil
}

func scanPolicyRevision(row pgx.Row) (application.PolicyRevision, error) {
	var v application.PolicyRevision
	var createdBy, activatedBy *string
	var activatedAt, retiredAt *time.Time
	err := row.Scan(&v.WorkspaceID, &v.ID, &v.Revision, &v.State, &v.MaxRiskLevel, &v.DenyUnsafeWrite, &v.DenyMCPUnsafeWrite, &createdBy, &v.CreatedAt, &activatedBy, &activatedAt, &retiredAt)
	if createdBy != nil {
		v.CreatedByUserID = *createdBy
	}
	if activatedBy != nil {
		v.ActivatedByUserID = *activatedBy
	}
	if activatedAt != nil {
		v.ActivatedAt = *activatedAt
	}
	if retiredAt != nil {
		v.RetiredAt = *retiredAt
	}
	return v, mapErr(err)
}

func scanDecision(row pgx.Row) (application.PolicyDecision, error) {
	var v application.PolicyDecision
	err := row.Scan(&v.Sequence, &v.WorkspaceID, &v.PolicyRevisionID, &v.PolicyRevision, &v.TargetKind, &v.TargetID, &v.TargetRevision, &v.RiskLevel, &v.Outcome, &v.ReasonCodes, &v.EvaluatedAt)
	return v, mapErr(err)
}

const policyRevisionCols = `workspace_id,id,revision,state,max_risk_level,deny_unsafe_write,deny_mcp_unsafe_write,created_by_user_id,created_at,activated_by_user_id,activated_at,retired_at`

func (r *PublicationPolicyRepository) ListPolicies(ctx context.Context, workspace string, limit int) (application.PolicySnapshot, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.PolicySnapshot{}, err
	}
	defer rollback(tx)
	revisions := []application.PolicyRevision{}
	rows, err := tx.Query(ctx, `SELECT `+policyRevisionCols+` FROM governance.catalog_publication_policy_revisions WHERE workspace_id=$1 ORDER BY revision DESC`, workspace)
	if err != nil {
		return application.PolicySnapshot{}, mapErr(err)
	}
	for rows.Next() {
		item, e := scanPolicyRevision(rows)
		if e != nil {
			rows.Close()
			return application.PolicySnapshot{}, e
		}
		revisions = append(revisions, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return application.PolicySnapshot{}, application.ErrUnavailable
	}
	decisions := []application.PolicyDecision{}
	rows, err = tx.Query(ctx, `SELECT sequence,workspace_id,policy_revision_id,policy_revision,target_kind,target_id,target_revision,risk_level,outcome,reason_codes,evaluated_at FROM governance.catalog_publication_policy_decisions WHERE workspace_id=$1 ORDER BY sequence DESC LIMIT $2`, workspace, limit)
	if err != nil {
		return application.PolicySnapshot{}, mapErr(err)
	}
	for rows.Next() {
		item, e := scanDecision(rows)
		if e != nil {
			rows.Close()
			return application.PolicySnapshot{}, e
		}
		decisions = append(decisions, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return application.PolicySnapshot{}, application.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return application.PolicySnapshot{}, application.ErrUnavailable
	}
	return application.PolicySnapshot{Revisions: revisions, Decisions: decisions}, nil
}

func (r *PublicationPolicyRepository) CreatePolicy(ctx context.Context, workspace, id, actor, maxRisk string, denyUnsafe, denyMCPUnsafe bool, at time.Time) (application.PolicyRevision, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.PolicyRevision{}, err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT governance.create_catalog_publication_policy($1,$2,$3,$4,$5,$6,$7)`, workspace, id, actor, maxRisk, denyUnsafe, denyMCPUnsafe, at); err != nil {
		return application.PolicyRevision{}, mapErr(err)
	}
	item, err := scanPolicyRevision(tx.QueryRow(ctx, `SELECT `+policyRevisionCols+` FROM governance.catalog_publication_policy_revisions WHERE workspace_id=$1 AND id=$2`, workspace, id))
	if err != nil {
		return application.PolicyRevision{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.PolicyRevision{}, application.ErrUnavailable
	}
	return item, nil
}

func (r *PublicationPolicyRepository) ActivatePolicy(ctx context.Context, workspace, id, actor string, at time.Time) (application.PolicyRevision, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.PolicyRevision{}, err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT governance.activate_catalog_publication_policy($1,$2,$3,$4)`, workspace, id, actor, at); err != nil {
		return application.PolicyRevision{}, mapErr(err)
	}
	item, err := scanPolicyRevision(tx.QueryRow(ctx, `SELECT `+policyRevisionCols+` FROM governance.catalog_publication_policy_revisions WHERE workspace_id=$1 AND id=$2`, workspace, id))
	if err != nil {
		return application.PolicyRevision{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.PolicyRevision{}, application.ErrUnavailable
	}
	return item, nil
}

var _ application.PolicyRepository = (*PublicationPolicyRepository)(nil)
