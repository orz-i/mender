package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

type Admissions struct{ tx pgx.Tx }

func NewAdmissions(tx pgx.Tx) *Admissions { return &Admissions{tx} }
func (a *Admissions) FindReplay(ctx context.Context, w, subject, key string) (application.AdmissionRecord, bool, error) {
	// A hash collision only serializes unrelated keys; the actual unique key is stored in full.
	_, e := a.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, w+"\x1f"+subject+"\x1f"+key)
	if e != nil {
		return application.AdmissionRecord{}, false, application.ErrAdmissionStorage
	}
	var r application.AdmissionRecord
	e = a.tx.QueryRow(ctx, `SELECT workspace_id,subject_id,credential_id,idempotency_key,request_hash,run_id,reservation_id,tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,currency,canonical_arguments,reserved_micro,created_at FROM execution.run_admissions WHERE workspace_id=$1 AND subject_id=$2 AND idempotency_key=$3`, w, subject, key).Scan(&r.WorkspaceID, &r.SubjectID, &r.CredentialID, &r.IdempotencyKey, &r.RequestHash, &r.RunID, &r.ReservationID, &r.ToolVersionID, &r.ToolsetVersionID, &r.ConnectionID, &r.PriceVersionID, &r.DeploymentRevision, &r.BudgetID, &r.PeriodID, &r.Currency, &r.CanonicalArguments, &r.ReservedMicro, &r.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, false, nil
	}
	if e != nil {
		return r, false, application.ErrAdmissionStorage
	}
	return r, true, nil
}
func (a *Admissions) InsertRun(ctx context.Context, r application.AdmissionRecord, run domain.Run) error {
	s := run.Snapshot()
	_, e := a.tx.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES($1,$2,$3,1,$4,$4)`, string(s.WorkspaceID), string(s.ID), string(s.State), s.CreatedAt)
	if e != nil {
		return application.ErrAdmissionStorage
	}
	_, e = a.tx.Exec(ctx, `INSERT INTO execution.run_admissions(workspace_id,subject_id,credential_id,idempotency_key,request_hash,run_id,reservation_id,tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,currency,canonical_arguments,reserved_micro,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`, r.WorkspaceID, r.SubjectID, r.CredentialID, r.IdempotencyKey, r.RequestHash, r.RunID, r.ReservationID, r.ToolVersionID, r.ToolsetVersionID, r.ConnectionID, r.PriceVersionID, r.DeploymentRevision, r.BudgetID, r.PeriodID, r.Currency, r.CanonicalArguments, r.ReservedMicro, r.CreatedAt)
	if e != nil {
		return application.ErrAdmissionStorage
	}
	return nil
}
func (a *Admissions) InsertJob(ctx context.Context, r application.AdmissionRecord) error {
	_, e := a.tx.Exec(ctx, `INSERT INTO execution.jobs(workspace_id,run_id,state,blocked_reason,created_at) VALUES($1,$2,'blocked','executor_not_configured',$3)`, r.WorkspaceID, r.RunID, r.CreatedAt)
	if e != nil {
		return application.ErrAdmissionStorage
	}
	return nil
}
func (a *Admissions) InsertOutbox(ctx context.Context, r application.AdmissionRecord) error {
	// Explicit integration projection: do not serialize arguments, keys or the domain aggregate.
	payload, e := json.Marshal(struct {
		RunID          string `json:"run_id"`
		ReservationID  string `json:"reservation_id"`
		ExecutionState string `json:"execution_state"`
		JobState       string `json:"job_state"`
	}{r.RunID, r.ReservationID, "queued", "blocked"})
	if e != nil {
		return application.ErrAdmissionStorage
	}
	_, e = a.tx.Exec(ctx, `INSERT INTO execution.outbox(workspace_id,event_id,run_id,event_type,schema_version,payload,occurred_at) VALUES($1,$2,$3,'run.admitted',1,$4,$5)`, r.WorkspaceID, "evt_"+r.RunID, r.RunID, string(payload), r.CreatedAt)
	if e != nil {
		return application.ErrAdmissionStorage
	}
	return nil
}

var _ application.AdmissionRepository = (*Admissions)(nil)
