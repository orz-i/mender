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

type HumanSessions struct{ pool *pgxpool.Pool }

func NewHumanSessions(pool *pgxpool.Pool) *HumanSessions { return &HumanSessions{pool: pool} }

func (r *HumanSessions) ResolveOIDCIdentity(ctx context.Context, issuer, subject string) (application.HumanIdentity, error) {
	var user application.HumanIdentity
	err := r.pool.QueryRow(ctx, `SELECT u.id,u.display_name,u.disabled FROM identity.oidc_identities o JOIN identity.users u ON u.id=o.user_id WHERE o.issuer=$1 AND o.subject=$2`, issuer, subject).Scan(&user.UserID, &user.DisplayName, &user.Disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.HumanIdentity{}, application.ErrNotFound
	}
	if err != nil {
		return application.HumanIdentity{}, application.ErrUnavailable
	}
	return user, nil
}

func (r *HumanSessions) CreateBrowserSession(ctx context.Context, session domain.HumanSession) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO identity.browser_sessions(digest,user_id,csrf_digest,created_at,expires_at) VALUES($1,$2,$3,$4,$5)`, session.Digest, session.UserID, session.CSRFDigest, session.CreatedAt, session.ExpiresAt)
	if err != nil {
		return application.ErrUnavailable
	}
	return nil
}

func (r *HumanSessions) FindBrowserSession(ctx context.Context, digest string) (domain.HumanSession, error) {
	var session domain.HumanSession
	err := r.pool.QueryRow(ctx, `SELECT s.digest,s.user_id,s.csrf_digest,s.created_at,s.expires_at,COALESCE(s.revoked_at,'0001-01-01T00:00:00Z'::timestamptz),u.disabled FROM identity.browser_sessions s JOIN identity.users u ON u.id=s.user_id WHERE s.digest=$1`, digest).Scan(&session.Digest, &session.UserID, &session.CSRFDigest, &session.CreatedAt, &session.ExpiresAt, &session.RevokedAt, &session.UserDisabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.HumanSession{}, application.ErrNotFound
	}
	if err != nil {
		return domain.HumanSession{}, application.ErrUnavailable
	}
	return session, nil
}

func (r *HumanSessions) RevokeBrowserSession(ctx context.Context, digest string, at time.Time) error {
	tag, err := r.pool.Exec(ctx, `UPDATE identity.browser_sessions SET revoked_at=$2 WHERE digest=$1 AND revoked_at IS NULL`, digest, at)
	if err != nil {
		return application.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return application.ErrNotFound
	}
	return nil
}

func (r *HumanSessions) ListWorkspaceMemberships(ctx context.Context, userID string) ([]domain.WorkspaceMembership, error) {
	rows, err := r.pool.Query(ctx, `SELECT m.workspace_id,m.user_id,m.role,m.disabled,w.disabled,m.created_at FROM identity.workspace_memberships m JOIN identity.workspaces w ON w.id=m.workspace_id WHERE m.user_id=$1 ORDER BY m.workspace_id`, userID)
	if err != nil {
		return nil, application.ErrUnavailable
	}
	defer rows.Close()
	var result []domain.WorkspaceMembership
	for rows.Next() {
		var m domain.WorkspaceMembership
		if err = rows.Scan(&m.WorkspaceID, &m.UserID, &m.Role, &m.Disabled, &m.WorkspaceDisabled, &m.CreatedAt); err != nil {
			return nil, application.ErrUnavailable
		}
		result = append(result, m)
	}
	if rows.Err() != nil {
		return nil, application.ErrUnavailable
	}
	return result, nil
}

func (r *HumanSessions) FindWorkspaceMembership(ctx context.Context, userID, workspace string) (domain.WorkspaceMembership, error) {
	var m domain.WorkspaceMembership
	err := r.pool.QueryRow(ctx, `SELECT m.workspace_id,m.user_id,m.role,m.disabled,w.disabled,m.created_at FROM identity.workspace_memberships m JOIN identity.workspaces w ON w.id=m.workspace_id WHERE m.user_id=$1 AND m.workspace_id=$2`, userID, workspace).Scan(&m.WorkspaceID, &m.UserID, &m.Role, &m.Disabled, &m.WorkspaceDisabled, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.WorkspaceMembership{}, application.ErrNotFound
	}
	if err != nil {
		return domain.WorkspaceMembership{}, application.ErrUnavailable
	}
	return m, nil
}

func (r *HumanSessions) FindPlatformStaff(ctx context.Context, userID string) (domain.PlatformStaff, error) {
	var staff domain.PlatformStaff
	err := r.pool.QueryRow(ctx, `SELECT user_id,role,disabled,created_at FROM identity.platform_staff WHERE user_id=$1`, userID).Scan(&staff.UserID, &staff.Role, &staff.Disabled, &staff.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PlatformStaff{}, application.ErrNotFound
	}
	if err != nil || !staff.Active() {
		return domain.PlatformStaff{}, application.ErrUnavailable
	}
	return staff, nil
}

var _ application.HumanSessionRepository = (*HumanSessions)(nil)
