package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

type ReleaseGovernanceRepository struct{ pool *pgxpool.Pool }

func NewReleaseGovernanceRepository(pool *pgxpool.Pool) *ReleaseGovernanceRepository {
	return &ReleaseGovernanceRepository{pool: pool}
}

func rollbackReleaseGovernance(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func releaseGovernanceError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ErrPublicationNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505", "23503", "23514", "40001":
			return application.ErrPublicationConflict
		case "22023":
			return application.ErrPublicationInvalid
		case "42501":
			return application.ErrPublicationForbidden
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return application.ErrPublicationUnavailable
}

func (r *ReleaseGovernanceRepository) beginRelease(ctx context.Context, workspace string) (pgx.Tx, error) {
	if r == nil || r.pool == nil {
		return nil, application.ErrPublicationUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrPublicationUnavailable
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace); err != nil {
		rollbackReleaseGovernance(tx)
		return nil, application.ErrPublicationUnavailable
	}
	return tx, nil
}

const releasePlanCols = `workspace_id,id,plugin_id,plugin_version,toolset_version_id,tool_version_id,provider_id,stable_deployment_revision,candidate_deployment_revision,revision,state,created_by_user_id,created_at,updated_at,canary_started_at,observation_until,activated_at,draining_at,rolled_back_at,disabled_at`

func scanReleasePlan(row pgx.Row) (application.ReleasePlan, error) {
	var v application.ReleasePlan
	var canary, observation, activated, draining, rolledBack, disabled *time.Time
	err := row.Scan(&v.WorkspaceID, &v.ID, &v.PluginID, &v.PluginVersion, &v.ToolsetVersionID, &v.ToolVersionID, &v.ProviderID,
		&v.StableDeploymentRevision, &v.CandidateDeploymentRevision, &v.Revision, &v.State, &v.CreatedByUserID, &v.CreatedAt, &v.UpdatedAt,
		&canary, &observation, &activated, &draining, &rolledBack, &disabled)
	if err != nil {
		return application.ReleasePlan{}, releaseGovernanceError(err)
	}
	if canary != nil {
		v.CanaryStartedAt = *canary
	}
	if observation != nil {
		v.ObservationUntil = *observation
	}
	if activated != nil {
		v.ActivatedAt = *activated
	}
	if draining != nil {
		v.DrainingAt = *draining
	}
	if rolledBack != nil {
		v.RolledBackAt = *rolledBack
	}
	if disabled != nil {
		v.DisabledAt = *disabled
	}
	return v, nil
}

func (r *ReleaseGovernanceRepository) planAfter(ctx context.Context, tx pgx.Tx, workspace, id string) (application.ReleasePlan, error) {
	return scanReleasePlan(tx.QueryRow(ctx, `SELECT `+releasePlanCols+` FROM supply.release_plans WHERE workspace_id=$1 AND id=$2`, workspace, id))
}

func (r *ReleaseGovernanceRepository) ReleaseSnapshot(ctx context.Context, workspace string) (application.ReleaseSnapshot, error) {
	tx, err := r.beginRelease(ctx, workspace)
	if err != nil {
		return application.ReleaseSnapshot{}, err
	}
	defer rollbackReleaseGovernance(tx)
	result := application.ReleaseSnapshot{Plans: []application.ReleasePlan{}, Routes: []application.ReleaseRoute{}, Events: []application.ReleaseAuditEvent{}}
	rows, err := tx.Query(ctx, `SELECT `+releasePlanCols+` FROM supply.release_plans WHERE workspace_id=$1 ORDER BY created_at DESC,id`, workspace)
	if err != nil {
		return result, releaseGovernanceError(err)
	}
	for rows.Next() {
		v, e := scanReleasePlan(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		result.Plans = append(result.Plans, v)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, application.ErrPublicationUnavailable
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT workspace_id,toolset_version_id,tool_version_id,stable_deployment_revision,coalesce(candidate_deployment_revision,''),mode,coalesce(release_plan_id,''),revision,updated_at FROM supply.release_routes WHERE workspace_id=$1 ORDER BY toolset_version_id,tool_version_id`, workspace)
	if err != nil {
		return result, releaseGovernanceError(err)
	}
	for rows.Next() {
		var v application.ReleaseRoute
		if err = rows.Scan(&v.WorkspaceID, &v.ToolsetVersionID, &v.ToolVersionID, &v.StableDeploymentRevision, &v.CandidateDeploymentRevision, &v.Mode, &v.ReleasePlanID, &v.Revision, &v.UpdatedAt); err != nil {
			rows.Close()
			return result, application.ErrPublicationUnavailable
		}
		result.Routes = append(result.Routes, v)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, application.ErrPublicationUnavailable
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT sequence,workspace_id,release_plan_id,plan_revision,route_revision,event_kind,route_mode,coalesce(selected_deployment_revision,''),actor_user_id,reason,occurred_at FROM supply.release_audit_events WHERE workspace_id=$1 ORDER BY sequence DESC LIMIT 200`, workspace)
	if err != nil {
		return result, releaseGovernanceError(err)
	}
	for rows.Next() {
		var v application.ReleaseAuditEvent
		if err = rows.Scan(&v.Sequence, &v.WorkspaceID, &v.ReleasePlanID, &v.PlanRevision, &v.RouteRevision, &v.EventKind, &v.RouteMode, &v.SelectedDeploymentRevision, &v.ActorUserID, &v.Reason, &v.OccurredAt); err != nil {
			rows.Close()
			return result, application.ErrPublicationUnavailable
		}
		result.Events = append(result.Events, v)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, application.ErrPublicationUnavailable
	}
	rows.Close()
	if err = tx.Commit(ctx); err != nil {
		return result, application.ErrPublicationUnavailable
	}
	return result, nil
}

func (r *ReleaseGovernanceRepository) CreateReleasePlan(ctx context.Context, workspace, id, actor string, input application.ReleasePlanInput, at time.Time) (application.ReleasePlan, error) {
	tx, err := r.beginRelease(ctx, workspace)
	if err != nil {
		return application.ReleasePlan{}, err
	}
	defer rollbackReleaseGovernance(tx)
	_, err = tx.Exec(ctx, `SELECT supply.create_release_plan($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, workspace, id, input.PluginID, input.PluginVersion, input.ToolsetVersionID, input.ToolVersionID, input.ProviderID, input.StableDeploymentRevision, input.CandidateDeploymentRevision, actor, input.Reason, at)
	if err != nil {
		return application.ReleasePlan{}, releaseGovernanceError(err)
	}
	v, err := r.planAfter(ctx, tx, workspace, id)
	if err != nil {
		return v, err
	}
	if err = tx.Commit(ctx); err != nil {
		return v, application.ErrPublicationUnavailable
	}
	return v, nil
}

func (r *ReleaseGovernanceRepository) releaseAction(ctx context.Context, workspace, id, actor, reason string, at time.Time, statement string, extra ...any) (application.ReleasePlan, error) {
	tx, err := r.beginRelease(ctx, workspace)
	if err != nil {
		return application.ReleasePlan{}, err
	}
	defer rollbackReleaseGovernance(tx)
	args := []any{workspace, id, actor, reason, at}
	args = append(args, extra...)
	if _, err = tx.Exec(ctx, statement, args...); err != nil {
		return application.ReleasePlan{}, releaseGovernanceError(err)
	}
	v, err := r.planAfter(ctx, tx, workspace, id)
	if err != nil {
		return v, err
	}
	if err = tx.Commit(ctx); err != nil {
		return v, application.ErrPublicationUnavailable
	}
	return v, nil
}

func (r *ReleaseGovernanceRepository) StartReleaseCanary(ctx context.Context, workspace, id, actor, reason string, at, until time.Time) (application.ReleasePlan, error) {
	return r.releaseAction(ctx, workspace, id, actor, reason, at, `SELECT supply.start_release_canary($1,$2,$3,$4,$5,$6)`, until)
}
func (r *ReleaseGovernanceRepository) PromoteRelease(ctx context.Context, workspace, id, actor, reason string, at time.Time) (application.ReleasePlan, error) {
	return r.releaseAction(ctx, workspace, id, actor, reason, at, `SELECT supply.promote_release($1,$2,$3,$4,$5)`)
}
func (r *ReleaseGovernanceRepository) DrainRelease(ctx context.Context, workspace, id, actor, reason string, at time.Time) (application.ReleasePlan, error) {
	return r.releaseAction(ctx, workspace, id, actor, reason, at, `SELECT supply.drain_release($1,$2,$3,$4,$5)`)
}
func (r *ReleaseGovernanceRepository) RollbackRelease(ctx context.Context, workspace, id, actor, reason string, at time.Time) (application.ReleasePlan, error) {
	return r.releaseAction(ctx, workspace, id, actor, reason, at, `SELECT supply.rollback_release($1,$2,$3,$4,$5)`)
}
func (r *ReleaseGovernanceRepository) EmergencyDisableRelease(ctx context.Context, workspace, id, actor, reason string, at time.Time) (application.ReleasePlan, error) {
	return r.releaseAction(ctx, workspace, id, actor, reason, at, `SELECT supply.emergency_disable_release($1,$2,$3,$4,$5)`)
}

var _ application.ReleaseGovernanceRepository = (*ReleaseGovernanceRepository)(nil)
