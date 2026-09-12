package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
)

type Observability struct{ pool *pgxpool.Pool }

func NewObservability(pool *pgxpool.Pool) *Observability { return &Observability{pool: pool} }

func rollback(tx pgx.Tx) { _ = tx.Rollback(context.Background()) }

func (r *Observability) Snapshot(ctx context.Context, workspace string, limit int) (application.UsageSnapshot, error) {
	if r == nil || r.pool == nil || limit < 1 || limit > 100 {
		return application.UsageSnapshot{}, application.ErrObservabilityUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return application.UsageSnapshot{}, application.ErrObservabilityUnavailable
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", workspace); err != nil {
		return application.UsageSnapshot{}, application.ErrObservabilityUnavailable
	}
	periodRows, err := tx.Query(ctx, `SELECT workspace_id,budget_id,period_id,currency,starts_at,ends_at,active,limit_micro,consumed_micro,reserved_micro,revision
		FROM commerce.budget_periods WHERE workspace_id=$1 ORDER BY starts_at DESC,budget_id COLLATE "C" ASC,period_id COLLATE "C" ASC LIMIT $2`, workspace, limit+1)
	if err != nil {
		return application.UsageSnapshot{}, application.ErrObservabilityUnavailable
	}
	periods := make([]application.BudgetPeriodView, 0)
	for periodRows.Next() {
		var v application.BudgetPeriodView
		if err = periodRows.Scan(&v.WorkspaceID, &v.BudgetID, &v.PeriodID, &v.Currency, &v.StartsAt, &v.EndsAt, &v.Active, &v.LimitMicro, &v.ConsumedMicro, &v.ReservedMicro, &v.Revision); err != nil {
			periodRows.Close()
			return application.UsageSnapshot{}, application.ErrObservabilityUnavailable
		}
		periods = append(periods, v)
	}
	if err = periodRows.Err(); err != nil {
		periodRows.Close()
		return application.UsageSnapshot{}, application.ErrObservabilityUnavailable
	}
	periodRows.Close()
	if len(periods) > limit {
		return application.UsageSnapshot{}, application.ErrObservabilityUnavailable
	}

	usageRows, err := tx.Query(ctx, `SELECT r.workspace_id,r.run_id,r.budget_id,r.period_id,r.currency,r.state,r.amount_micro,r.charged_micro,r.created_at,
		COALESCE(r.settled_at,r.released_at,'0001-01-01T00:00:00Z'::timestamptz),COALESCE(s.outcome,'')
		FROM commerce.reservations r LEFT JOIN commerce.usage_settlements s ON s.workspace_id=r.workspace_id AND s.run_id=r.run_id
		WHERE r.workspace_id=$1 ORDER BY r.created_at DESC,r.run_id COLLATE "C" DESC LIMIT $2`, workspace, limit+1)
	if err != nil {
		return application.UsageSnapshot{}, application.ErrObservabilityUnavailable
	}
	entries := make([]application.UsageEntryView, 0)
	for usageRows.Next() {
		var v application.UsageEntryView
		if err = usageRows.Scan(&v.WorkspaceID, &v.RunID, &v.BudgetID, &v.PeriodID, &v.Currency, &v.QuotaState, &v.ReservedMicro, &v.ChargedMicro, &v.CreatedAt, &v.FinalizedAt, &v.Outcome); err != nil {
			usageRows.Close()
			return application.UsageSnapshot{}, application.ErrObservabilityUnavailable
		}
		entries = append(entries, v)
	}
	if err = usageRows.Err(); err != nil {
		usageRows.Close()
		return application.UsageSnapshot{}, application.ErrObservabilityUnavailable
	}
	usageRows.Close()
	if len(entries) > limit {
		return application.UsageSnapshot{}, application.ErrObservabilityUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return application.UsageSnapshot{}, application.ErrObservabilityUnavailable
	}
	return application.UsageSnapshot{BudgetPeriods: periods, Entries: entries}, nil
}

var _ application.UsageRepository = (*Observability)(nil)

func (r *Observability) RunCost(ctx context.Context, workspace, runID string) (application.UsageEntryView, error) {
	if r == nil || r.pool == nil {
		return application.UsageEntryView{}, application.ErrObservabilityUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return application.UsageEntryView{}, application.ErrObservabilityUnavailable
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", workspace); err != nil {
		return application.UsageEntryView{}, application.ErrObservabilityUnavailable
	}
	var v application.UsageEntryView
	err = tx.QueryRow(ctx, `SELECT r.workspace_id,r.run_id,r.budget_id,r.period_id,r.currency,r.state,r.amount_micro,r.charged_micro,r.created_at,
		COALESCE(r.settled_at,r.released_at,'0001-01-01T00:00:00Z'::timestamptz),COALESCE(s.outcome,'')
		FROM commerce.reservations r LEFT JOIN commerce.usage_settlements s ON s.workspace_id=r.workspace_id AND s.run_id=r.run_id
		WHERE r.workspace_id=$1 AND r.run_id=$2`, workspace, runID).Scan(
		&v.WorkspaceID, &v.RunID, &v.BudgetID, &v.PeriodID, &v.Currency, &v.QuotaState, &v.ReservedMicro, &v.ChargedMicro, &v.CreatedAt, &v.FinalizedAt, &v.Outcome,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.UsageEntryView{}, application.ErrObservabilityNotFound
	}
	if err != nil {
		return application.UsageEntryView{}, application.ErrObservabilityUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return application.UsageEntryView{}, application.ErrObservabilityUnavailable
	}
	return v, nil
}

var _ application.RunCostRepository = (*Observability)(nil)
