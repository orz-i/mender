package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	"time"
)

// Cancellations only writes execution-owned facts. Lifetime and quota release
// belong to the dedicated outer UoW, not to this repository.
type Cancellations struct{ tx pgx.Tx }

func NewCancellations(tx pgx.Tx) *Cancellations { return &Cancellations{tx} }
func (r *Cancellations) FindReference(ctx context.Context, w, id string) (application.CancellationRef, bool, error) {
	var q application.CancellationRef
	e := r.tx.QueryRow(ctx, `SELECT workspace_id,run_id,reservation_id,budget_id,period_id,currency,reserved_micro FROM execution.run_admissions WHERE workspace_id=$1 AND run_id=$2`, w, id).Scan(&q.WorkspaceID, &q.RunID, &q.ReservationID, &q.BudgetID, &q.PeriodID, &q.Currency, &q.AmountMicro)
	if errors.Is(e, pgx.ErrNoRows) {
		return q, false, nil
	}
	if e != nil {
		return q, false, application.ErrCancellationStorage
	}
	return q, true, nil
}
func (r *Cancellations) LoadCancellation(ctx context.Context, q application.CancellationRef) (application.CancellationData, error) {
	var d application.CancellationData
	var s domain.Snapshot
	var w, id, state string
	var version int64
	e := r.tx.QueryRow(ctx, `SELECT workspace_id,id,state,version,created_at,updated_at FROM execution.runs WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, q.WorkspaceID, q.RunID).Scan(&w, &id, &state, &version, &s.CreatedAt, &s.UpdatedAt)
	if e != nil || version < 1 {
		return d, application.ErrCancellationStorage
	}
	s.ID = domain.RunID(id)
	s.WorkspaceID = domain.WorkspaceID(w)
	s.State = domain.State(state)
	s.Version = uint64(version)
	d.Run, e = domain.Restore(s)
	if e != nil {
		return d, application.ErrCancellationStorage
	}
	var stopped *time.Time
	e = r.tx.QueryRow(ctx, `SELECT state,blocked_reason,created_at,stopped_at FROM execution.jobs WHERE workspace_id=$1 AND run_id=$2 FOR UPDATE`, q.WorkspaceID, q.RunID).Scan(&d.JobState, &d.BlockedReason, &d.JobCreatedAt, &stopped)
	if e != nil {
		return d, application.ErrCancellationStorage
	}
	if stopped != nil {
		d.StoppedAt = *stopped
	}
	e = r.tx.QueryRow(ctx, `SELECT delivery_state FROM execution.outbox WHERE workspace_id=$1 AND run_id=$2 AND event_type='run.admitted' FOR UPDATE`, q.WorkspaceID, q.RunID).Scan(&d.AdmissionDelivery)
	if e != nil {
		return d, application.ErrCancellationStorage
	}
	var rv int64
	e = r.tx.QueryRow(ctx, `SELECT workspace_id,run_id,reservation_id,budget_id,period_id,currency,released_micro,version,subject_id,credential_id,reason,occurred_at FROM execution.run_cancellations WHERE workspace_id=$1 AND run_id=$2`, q.WorkspaceID, q.RunID).Scan(&d.ReceiptRef.WorkspaceID, &d.ReceiptRef.RunID, &d.ReceiptRef.ReservationID, &d.ReceiptRef.BudgetID, &d.ReceiptRef.PeriodID, &d.ReceiptRef.Currency, &d.ReceiptRef.AmountMicro, &rv, &d.ReceiptSubject, &d.ReceiptCredential, &d.ReceiptReason, &d.ReceiptAt)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return d, application.ErrCancellationStorage
	}
	d.HasReceipt = e == nil
	if d.HasReceipt {
		if rv < 1 {
			return d, application.ErrCancellationStorage
		}
		d.ReceiptVersion = uint64(rv)
		e = r.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution.run_events e JOIN execution.outbox o ON o.workspace_id=e.workspace_id AND o.run_id=e.run_id AND o.event_type='run.canceled' WHERE e.workspace_id=$1 AND e.run_id=$2 AND e.version=$3 AND e.state='canceled' AND e.subject_id=$4 AND e.credential_id=$5 AND e.reason=$6 AND e.occurred_at=$7 AND o.occurred_at=$7)`, q.WorkspaceID, q.RunID, rv, d.ReceiptSubject, d.ReceiptCredential, d.ReceiptReason, d.ReceiptAt).Scan(&d.CanceledEventMatches)
	} else {
		e = r.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution.outbox WHERE workspace_id=$1 AND run_id=$2 AND event_type='run.canceled')`, q.WorkspaceID, q.RunID).Scan(&d.CanceledEventMatches)
	}
	if e != nil {
		return d, application.ErrCancellationStorage
	}
	return d, nil
}
func (r *Cancellations) SaveCancellation(ctx context.Context, q application.CancellationRef, run domain.Run, expected uint64, ch application.CancellationChange) error {
	s := run.Snapshot()
	if expected != 1 || s.Version != 2 || s.State != domain.Canceled || string(s.ID) != q.RunID || string(s.WorkspaceID) != q.WorkspaceID {
		return application.ErrUnsafeCancellation
	}
	tag, e := r.tx.Exec(ctx, `UPDATE execution.jobs SET state='canceled',stopped_at=$1 WHERE workspace_id=$2 AND run_id=$3 AND state='blocked' AND blocked_reason='executor_not_configured' AND stopped_at IS NULL`, ch.At, q.WorkspaceID, q.RunID)
	if e != nil || tag.RowsAffected() != 1 {
		return application.ErrCancellationStorage
	}
	tag, e = r.tx.Exec(ctx, `UPDATE execution.runs SET state='canceled',version=$1,updated_at=$2 WHERE workspace_id=$3 AND id=$4 AND version=$5 AND state='queued' AND created_at=$6 AND updated_at<=$2`, int64(s.Version), ch.At, q.WorkspaceID, q.RunID, int64(expected), s.CreatedAt)
	if e != nil || tag.RowsAffected() != 1 {
		return application.ErrCancellationStorage
	}
	_, e = r.tx.Exec(ctx, `INSERT INTO execution.run_events(workspace_id,run_id,version,state,subject_id,credential_id,occurred_at,reason) VALUES($1,$2,$3,'canceled',$4,$5,$6,$7)`, q.WorkspaceID, q.RunID, int64(s.Version), ch.SubjectID, ch.CredentialID, ch.At, ch.Reason)
	if e != nil {
		return application.ErrCancellationStorage
	}
	tag, e = r.tx.Exec(ctx, `UPDATE execution.outbox SET delivery_state='suppressed' WHERE workspace_id=$1 AND run_id=$2 AND event_type='run.admitted' AND delivery_state='pending'`, q.WorkspaceID, q.RunID)
	if e != nil || tag.RowsAffected() != 1 {
		return application.ErrCancellationStorage
	}
	payload, e := json.Marshal(struct {
		RunID          string `json:"run_id"`
		ExecutionState string `json:"execution_state"`
		JobState       string `json:"job_state"`
	}{q.RunID, "canceled", "canceled"})
	if e != nil {
		return application.ErrCancellationStorage
	}
	_, e = r.tx.Exec(ctx, `INSERT INTO execution.outbox(workspace_id,event_id,run_id,event_type,schema_version,payload,occurred_at) VALUES($1,$2,$3,'run.canceled',1,$4,$5)`, q.WorkspaceID, "evt_cancel_"+q.RunID, q.RunID, string(payload), ch.At)
	if e != nil {
		return application.ErrCancellationStorage
	}
	_, e = r.tx.Exec(ctx, `INSERT INTO execution.run_cancellations(workspace_id,run_id,reservation_id,budget_id,period_id,currency,released_micro,version,subject_id,credential_id,reason,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, q.WorkspaceID, q.RunID, q.ReservationID, q.BudgetID, q.PeriodID, q.Currency, q.AmountMicro, int64(s.Version), ch.SubjectID, ch.CredentialID, ch.Reason, ch.At)
	if e != nil {
		return application.ErrCancellationStorage
	}
	return nil
}

var _ application.CancellationRepository = (*Cancellations)(nil)
