package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

type PlatformAdminRepository struct{ pool *pgxpool.Pool }

func NewPlatformAdmin(pool *pgxpool.Pool) *PlatformAdminRepository {
	return &PlatformAdminRepository{pool: pool}
}

func platformAdminError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "22023":
			return application.ErrInvalid
		case "23505", "23514", "40001":
			return application.ErrConflict
		case "42501":
			return application.ErrForbidden
		case "P0002":
			return application.ErrNotFound
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return application.ErrUnavailable
}

func scanPlatformWorkspace(row pgx.Row) (application.PlatformWorkspace, error) {
	var item application.PlatformWorkspace
	err := row.Scan(&item.WorkspaceID, &item.Frozen, &item.Revision, &item.Reason, &item.ActorUserID, &item.UpdatedAt, &item.CreatedAt)
	return item, platformAdminError(err)
}

func (r *PlatformAdminRepository) ListPlatformWorkspaces(ctx context.Context, actor string) ([]application.PlatformWorkspace, error) {
	rows, err := r.pool.Query(ctx, `SELECT * FROM governance.platform_admin_workspaces($1)`, actor)
	if err != nil {
		return nil, platformAdminError(err)
	}
	defer rows.Close()
	items := []application.PlatformWorkspace{}
	for rows.Next() {
		item, scanErr := scanPlatformWorkspace(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, application.ErrUnavailable
	}
	return items, nil
}

func (r *PlatformAdminRepository) SetPlatformWorkspaceFrozen(ctx context.Context, workspace string, expected int64, frozen bool, actor, reason string, at time.Time) (application.PlatformWorkspace, error) {
	return scanPlatformWorkspace(r.pool.QueryRow(ctx, `SELECT * FROM governance.platform_admin_set_workspace_frozen($1,$2,$3,$4,$5,$6)`, workspace, expected, frozen, actor, reason, at))
}

func scanPlatformProvider(row pgx.Row) (application.PlatformProvider, error) {
	var item application.PlatformProvider
	err := row.Scan(&item.ProviderID, &item.State, &item.Revision, &item.Reason, &item.ActorUserID, &item.UpdatedAt, &item.DeploymentCount, &item.ActiveDeploymentCount)
	return item, platformAdminError(err)
}

func (r *PlatformAdminRepository) ListPlatformProviders(ctx context.Context, actor string) ([]application.PlatformProvider, error) {
	rows, err := r.pool.Query(ctx, `SELECT * FROM governance.platform_admin_providers($1)`, actor)
	if err != nil {
		return nil, platformAdminError(err)
	}
	defer rows.Close()
	items := []application.PlatformProvider{}
	for rows.Next() {
		item, scanErr := scanPlatformProvider(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, application.ErrUnavailable
	}
	return items, nil
}

func (r *PlatformAdminRepository) SetPlatformProviderState(ctx context.Context, provider string, expected int64, state, actor, reason string, at time.Time) (application.PlatformProvider, error) {
	return scanPlatformProvider(r.pool.QueryRow(ctx, `SELECT * FROM governance.platform_admin_set_provider_state($1,$2,$3,$4,$5,$6)`, provider, expected, state, actor, reason, at))
}

func scanPlatformIncident(row pgx.Row) (application.PlatformIncident, error) {
	var item application.PlatformIncident
	var resolvedAt *time.Time
	err := row.Scan(&item.ID, &item.TargetKind, &item.TargetID, &item.Severity, &item.Code, &item.State, &item.OpenedByUserID, &item.OpenReason, &item.ResolvedByUserID, &item.Resolution, &item.Revision, &item.OpenedAt, &item.UpdatedAt, &resolvedAt)
	if resolvedAt != nil {
		item.ResolvedAt = *resolvedAt
	}
	return item, platformAdminError(err)
}

func (r *PlatformAdminRepository) OpenPlatformIncident(ctx context.Context, id, targetKind, targetID, severity, code, actor, reason string, at time.Time) (application.PlatformIncident, error) {
	return scanPlatformIncident(r.pool.QueryRow(ctx, `SELECT * FROM governance.platform_admin_open_incident($1,$2,$3,$4,$5,$6,$7,$8)`, id, targetKind, targetID, severity, code, actor, reason, at))
}

func (r *PlatformAdminRepository) ResolvePlatformIncident(ctx context.Context, id string, expected int64, actor, resolution string, at time.Time) (application.PlatformIncident, error) {
	return scanPlatformIncident(r.pool.QueryRow(ctx, `SELECT * FROM governance.platform_admin_resolve_incident($1,$2,$3,$4,$5)`, id, expected, actor, resolution, at))
}

func (r *PlatformAdminRepository) ListPlatformIncidents(ctx context.Context, actor string, filter application.PlatformIncidentFilter) ([]application.PlatformIncident, error) {
	var before any
	if !filter.BeforeUpdatedAt.IsZero() {
		before = filter.BeforeUpdatedAt
	}
	rows, err := r.pool.Query(ctx, `SELECT * FROM governance.platform_admin_incidents($1,$2,$3,$4,$5,$6,$7)`, actor, filter.State, filter.TargetKind, filter.TargetID, before, filter.BeforeID, filter.Limit)
	if err != nil {
		return nil, platformAdminError(err)
	}
	defer rows.Close()
	items := []application.PlatformIncident{}
	for rows.Next() {
		item, scanErr := scanPlatformIncident(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, application.ErrUnavailable
	}
	return items, nil
}

func (r *PlatformAdminRepository) ListPlatformAudit(ctx context.Context, actor string, filter application.PlatformAuditFilter) ([]application.PlatformAdminAuditEvent, error) {
	rows, err := r.pool.Query(ctx, `SELECT * FROM governance.platform_admin_audit_export($1,$2,$3)`, actor, filter.BeforeSequence, filter.Limit)
	if err != nil {
		return nil, platformAdminError(err)
	}
	defer rows.Close()
	items := []application.PlatformAdminAuditEvent{}
	for rows.Next() {
		var item application.PlatformAdminAuditEvent
		if err = rows.Scan(&item.Sequence, &item.EventKind, &item.TargetKind, &item.TargetID, &item.TargetRevision, &item.ActorUserID, &item.Reason, &item.OccurredAt); err != nil {
			return nil, application.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, application.ErrUnavailable
	}
	return items, nil
}

var _ application.PlatformAdminRepository = (*PlatformAdminRepository)(nil)
