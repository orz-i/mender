package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/identity/application"
	"github.com/orz-i/mender/backend/internal/contexts/identity/domain"
)

type RunStartDelegations struct {
	reader *pgxpool.Pool
	writer *pgxpool.Pool
}

func NewRunStartDelegations(reader, writer *pgxpool.Pool) *RunStartDelegations {
	return &RunStartDelegations{reader: reader, writer: writer}
}

func (r *RunStartDelegations) CreateRunStartDelegation(ctx context.Context, d domain.RunStartDelegation) error {
	if r == nil || r.writer == nil || !d.Validate() {
		return application.ErrUnavailable
	}
	_, err := r.writer.Exec(ctx, `INSERT INTO identity.run_start_delegations(id,digest,workspace_id,user_id,toolset_version_id,tool_id,tool_version,tool_version_id,connection_id,currency,max_charge_micro,idempotency_key,arguments_hash,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, d.ID, d.Digest, d.WorkspaceID, d.UserID, d.ToolsetVersionID, d.ToolID, d.ToolVersion, d.ToolVersionID, d.ConnectionID, d.Currency, d.MaxChargeMicro, d.IdempotencyKey, d.ArgumentsHash, d.CreatedAt, d.ExpiresAt)
	if err != nil {
		return application.ErrUnavailable
	}
	return nil
}

const startDelegationProjection = `SELECT d.id,d.digest,d.workspace_id,d.user_id,d.toolset_version_id,d.tool_id,d.tool_version,d.tool_version_id,d.connection_id,d.currency,d.max_charge_micro,d.idempotency_key,d.arguments_hash,d.created_at,d.expires_at,COALESCE(d.revoked_at,'0001-01-01T00:00:00Z'::timestamptz),u.disabled,m.role,m.disabled,w.disabled
FROM identity.run_start_delegations d
JOIN identity.users u ON u.id=d.user_id
JOIN identity.workspace_memberships m ON m.workspace_id=d.workspace_id AND m.user_id=d.user_id
JOIN identity.workspaces w ON w.id=d.workspace_id`

func scanRunStartDelegation(row pgx.Row) (domain.RunStartDelegation, error) {
	var d domain.RunStartDelegation
	err := row.Scan(&d.ID, &d.Digest, &d.WorkspaceID, &d.UserID, &d.ToolsetVersionID, &d.ToolID, &d.ToolVersion, &d.ToolVersionID, &d.ConnectionID, &d.Currency, &d.MaxChargeMicro, &d.IdempotencyKey, &d.ArgumentsHash, &d.CreatedAt, &d.ExpiresAt, &d.RevokedAt, &d.UserDisabled, &d.MembershipRole, &d.MembershipDisabled, &d.WorkspaceDisabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RunStartDelegation{}, application.ErrNotFound
	}
	if err != nil || !d.Validate() {
		return domain.RunStartDelegation{}, application.ErrUnavailable
	}
	return d, nil
}

func (r *RunStartDelegations) FindRunStartDelegationByDigest(ctx context.Context, digest string) (domain.RunStartDelegation, error) {
	if r == nil || r.reader == nil {
		return domain.RunStartDelegation{}, application.ErrUnavailable
	}
	return scanRunStartDelegation(r.reader.QueryRow(ctx, startDelegationProjection+` WHERE d.digest=$1`, digest))
}

func (r *RunStartDelegations) FindRunStartDelegationByID(ctx context.Context, id string) (domain.RunStartDelegation, error) {
	if r == nil || r.reader == nil {
		return domain.RunStartDelegation{}, application.ErrUnavailable
	}
	return scanRunStartDelegation(r.reader.QueryRow(ctx, startDelegationProjection+` WHERE d.id=$1`, id))
}

func (r *RunStartDelegations) RevokeRunStartDelegation(ctx context.Context, id, workspace, user string, at time.Time) error {
	if r == nil || r.writer == nil {
		return application.ErrUnavailable
	}
	tag, err := r.writer.Exec(ctx, `UPDATE identity.run_start_delegations SET revoked_at=$4 WHERE id=$1 AND workspace_id=$2 AND user_id=$3 AND revoked_at IS NULL`, id, workspace, user, at)
	if err != nil {
		return application.ErrUnavailable
	}
	if tag.RowsAffected() == 0 {
		return application.ErrNotFound
	}
	return nil
}

var _ application.RunStartDelegationRepository = (*RunStartDelegations)(nil)
