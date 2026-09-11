package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/orz-i/mender/backend/internal/processes/settlement/application"
)

type Jobs struct {
	tx        pgx.Tx
	workspace string
}

func NewJobs(tx pgx.Tx, workspace string) *Jobs { return &Jobs{tx: tx, workspace: workspace} }
func (j *Jobs) Claim(ctx context.Context) (application.Candidate, bool, error) {
	var c application.Candidate
	err := j.tx.QueryRow(ctx, `SELECT s.workspace_id,s.run_id,s.observation_id,a.reservation_id,a.price_version_id,a.budget_id,a.period_id,a.currency,a.reserved_micro,p.state,p.observed_at
FROM execution.settlement_jobs s
JOIN execution.run_admissions a ON (a.workspace_id,a.run_id)=(s.workspace_id,s.run_id)
JOIN execution.provider_observations p ON (p.workspace_id,p.run_id,p.observation_id)=(s.workspace_id,s.run_id,s.observation_id)
WHERE s.workspace_id=$1 AND s.state='pending'
ORDER BY s.created_at,s.run_id LIMIT 1 FOR UPDATE OF s SKIP LOCKED`, j.workspace).Scan(&c.WorkspaceID, &c.RunID, &c.ObservationID, &c.ReservationID, &c.PriceVersionID, &c.BudgetID, &c.PeriodID, &c.Currency, &c.ReservedMicro, &c.Outcome, &c.ObservedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.Candidate{}, false, nil
	}
	if err != nil {
		return application.Candidate{}, false, application.ErrUnavailable
	}
	return c, true, nil
}
func (j *Jobs) Finish(ctx context.Context, c application.Candidate, at time.Time) error {
	if j == nil || j.tx == nil || c.WorkspaceID != j.workspace || at.IsZero() {
		return application.ErrInvalid
	}
	tag, err := j.tx.Exec(ctx, `UPDATE execution.settlement_jobs SET state='finished',finished_at=$1 WHERE workspace_id=$2 AND run_id=$3 AND observation_id=$4 AND state='pending'`, at, c.WorkspaceID, c.RunID, c.ObservationID)
	if err != nil || tag.RowsAffected() != 1 {
		return application.ErrUnavailable
	}
	return nil
}
