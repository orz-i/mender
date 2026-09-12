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

// RunDelegations uses the ordinary Run API role for verification reads and
// the dedicated browser-session role for issuance/revocation writes.
type RunDelegations struct {
	reader *pgxpool.Pool
	writer *pgxpool.Pool
}

func NewRunDelegations(reader, writer *pgxpool.Pool) *RunDelegations {
	return &RunDelegations{reader: reader, writer: writer}
}

func (r *RunDelegations) CreateRunDelegation(ctx context.Context, d domain.RunDelegation) error {
	if r == nil || r.writer == nil || !d.Validate() {
		return application.ErrUnavailable
	}
	_, err := r.writer.Exec(ctx, `INSERT INTO identity.run_delegations(id,digest,workspace_id,user_id,scopes,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, d.ID, d.Digest, d.WorkspaceID, d.UserID, d.Scopes, d.CreatedAt, d.ExpiresAt)
	if err != nil {
		return application.ErrUnavailable
	}
	return nil
}

func scanRunDelegation(row pgx.Row) (domain.RunDelegation, error) {
	var d domain.RunDelegation
	err := row.Scan(&d.ID, &d.Digest, &d.WorkspaceID, &d.UserID, &d.Scopes, &d.CreatedAt, &d.ExpiresAt, &d.RevokedAt, &d.UserDisabled, &d.MembershipRole, &d.MembershipDisabled, &d.WorkspaceDisabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RunDelegation{}, application.ErrNotFound
	}
	if err != nil || !d.Validate() {
		return domain.RunDelegation{}, application.ErrUnavailable
	}
	return d, nil
}

const delegationProjection = `SELECT d.id,d.digest,d.workspace_id,d.user_id,d.scopes,d.created_at,d.expires_at,COALESCE(d.revoked_at,'0001-01-01T00:00:00Z'::timestamptz),u.disabled,m.role,m.disabled,w.disabled
FROM identity.run_delegations d
JOIN identity.users u ON u.id=d.user_id
JOIN identity.workspace_memberships m ON m.workspace_id=d.workspace_id AND m.user_id=d.user_id
JOIN identity.workspaces w ON w.id=d.workspace_id`

func (r *RunDelegations) FindRunDelegationByDigest(ctx context.Context, digest string) (domain.RunDelegation, error) {
	if r == nil || r.reader == nil {
		return domain.RunDelegation{}, application.ErrUnavailable
	}
	return scanRunDelegation(r.reader.QueryRow(ctx, delegationProjection+` WHERE d.digest=$1`, digest))
}

func (r *RunDelegations) FindRunDelegationByID(ctx context.Context, id string) (domain.RunDelegation, error) {
	if r == nil || r.reader == nil {
		return domain.RunDelegation{}, application.ErrUnavailable
	}
	return scanRunDelegation(r.reader.QueryRow(ctx, delegationProjection+` WHERE d.id=$1`, id))
}

func (r *RunDelegations) RevokeRunDelegation(ctx context.Context, id, workspace, user string, at time.Time) error {
	if r == nil || r.writer == nil {
		return application.ErrUnavailable
	}
	tag, err := r.writer.Exec(ctx, `UPDATE identity.run_delegations SET revoked_at=$4 WHERE id=$1 AND workspace_id=$2 AND user_id=$3 AND revoked_at IS NULL`, id, workspace, user, at)
	if err != nil {
		return application.ErrUnavailable
	}
	if tag.RowsAffected() == 0 {
		return application.ErrNotFound
	}
	return nil
}

var _ application.RunDelegationRepository = (*RunDelegations)(nil)
