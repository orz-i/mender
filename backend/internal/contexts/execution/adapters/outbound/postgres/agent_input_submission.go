package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

type AgentInputs struct{ pool *pgxpool.Pool }

func NewAgentInputs(pool *pgxpool.Pool) *AgentInputs { return &AgentInputs{pool: pool} }

func (r *AgentInputs) scoped(ctx context.Context, workspace domain.WorkspaceID) (pgx.Tx, error) {
	if r == nil || r.pool == nil || !workspace.IsValid() {
		return nil, application.ErrAgentInputUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrAgentInputUnavailable
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", string(workspace)); err != nil {
		rollback(tx)
		return nil, application.ErrAgentInputUnavailable
	}
	return tx, nil
}

func scanAgentInputTarget(row rowScanner) (application.AgentInputSubmissionTarget, error) {
	var target application.AgentInputSubmissionTarget
	var attempt int32
	err := row.Scan(&target.WorkspaceID, &target.RunID, &attempt, &target.InputRequestID, &target.ProviderID, &target.ProviderRequestID, &target.ExternalTaskID, &target.InputSchemaJSON, &target.State, &target.AnswerSHA256, &target.SubmissionID)
	if err == nil && attempt > 0 {
		target.AttemptNo = uint32(attempt)
	}
	return target, err
}

func scanAgentInputSubmissionRecord(row rowScanner) (application.AgentInputSubmissionRecord, error) {
	var record application.AgentInputSubmissionRecord
	return record, row.Scan(&record.WorkspaceID, &record.RunID, &record.InputRequestID, &record.State, &record.RequestedAt, &record.UpdatedAt)
}

const agentInputTargetSQL = `SELECT workspace_id,run_id,attempt_no,input_request_id,provider_id,provider_request_id,external_task_id,input_schema_json::text,state,coalesce(answer_sha256,''),coalesce(submission_id,'') FROM execution.agent_input_requests WHERE workspace_id=$1 AND run_id=$2 AND input_request_id=$3`

func (r *AgentInputs) FindAgentInputTarget(ctx context.Context, workspace domain.WorkspaceID, runID domain.RunID, inputRequestID string) (application.AgentInputSubmissionTarget, error) {
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return application.AgentInputSubmissionTarget{}, err
	}
	defer rollback(tx)
	target, err := scanAgentInputTarget(tx.QueryRow(ctx, agentInputTargetSQL, string(workspace), string(runID), inputRequestID))
	if errors.Is(err, pgx.ErrNoRows) {
		return application.AgentInputSubmissionTarget{}, application.ErrNoAgentInput
	}
	if err != nil || !target.Valid() || target.WorkspaceID != workspace || target.RunID != runID || target.InputRequestID != inputRequestID {
		return application.AgentInputSubmissionTarget{}, application.ErrAgentInputUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return application.AgentInputSubmissionTarget{}, application.ErrAgentInputUnavailable
	}
	return target, nil
}

func exactAgentInputTarget(left, right application.AgentInputSubmissionTarget) bool {
	return left.WorkspaceID == right.WorkspaceID && left.RunID == right.RunID && left.AttemptNo == right.AttemptNo && left.ProviderID == right.ProviderID && left.ProviderRequestID == right.ProviderRequestID && left.ExternalTaskID == right.ExternalTaskID && left.InputRequestID == right.InputRequestID && left.InputSchemaJSON == right.InputSchemaJSON
}

func loadLockedAgentInput(ctx context.Context, tx pgx.Tx, target application.AgentInputSubmissionTarget) (application.AgentInputSubmissionTarget, application.AgentInputSubmissionRecord, error) {
	current, err := scanAgentInputTarget(tx.QueryRow(ctx, agentInputTargetSQL+` FOR UPDATE`, string(target.WorkspaceID), string(target.RunID), target.InputRequestID))
	if errors.Is(err, pgx.ErrNoRows) {
		return application.AgentInputSubmissionTarget{}, application.AgentInputSubmissionRecord{}, application.ErrAgentInputConflict
	}
	if err != nil || !current.Valid() || !exactAgentInputTarget(current, target) {
		return application.AgentInputSubmissionTarget{}, application.AgentInputSubmissionRecord{}, application.ErrAgentInputConflict
	}
	record, err := scanAgentInputSubmissionRecord(tx.QueryRow(ctx, `SELECT workspace_id,run_id,input_request_id,state,requested_at,updated_at FROM execution.agent_input_requests WHERE workspace_id=$1 AND run_id=$2 AND input_request_id=$3`, string(target.WorkspaceID), string(target.RunID), target.InputRequestID))
	if err != nil {
		return application.AgentInputSubmissionTarget{}, application.AgentInputSubmissionRecord{}, application.ErrAgentInputUnavailable
	}
	return current, record, nil
}

func verifyAgentInputAttempt(ctx context.Context, tx pgx.Tx, target application.AgentInputSubmissionTarget) error {
	var state, providerID, providerRequestID string
	var externalTaskID *string
	err := tx.QueryRow(ctx, `SELECT state,provider_id,provider_request_id,external_task_id FROM execution.run_attempts WHERE workspace_id=$1 AND run_id=$2 AND attempt_no=$3`, string(target.WorkspaceID), string(target.RunID), int32(target.AttemptNo)).Scan(&state, &providerID, &providerRequestID, &externalTaskID)
	if err != nil || state != string(domain.AttemptSubmitted) && state != string(domain.AttemptUnknown) || providerID != target.ProviderID || providerRequestID != target.ProviderRequestID || externalTaskID == nil || *externalTaskID != target.ExternalTaskID {
		return application.ErrAgentInputConflict
	}
	return nil
}

func (r *AgentInputs) ClaimAgentInput(ctx context.Context, target application.AgentInputSubmissionTarget, prepared application.PreparedAgentInput, at time.Time) (application.AgentInputClaim, error) {
	if !target.Valid() || !prepared.Valid() || at.IsZero() {
		return application.AgentInputClaim{}, application.ErrAgentInputInvalid
	}
	tx, err := r.scoped(ctx, target.WorkspaceID)
	if err != nil {
		return application.AgentInputClaim{}, err
	}
	defer rollback(tx)
	current, record, err := loadLockedAgentInput(ctx, tx, target)
	if err != nil {
		return application.AgentInputClaim{}, err
	}
	if err = verifyAgentInputAttempt(ctx, tx, current); err != nil {
		return application.AgentInputClaim{}, err
	}
	run, err := scanRun(tx.QueryRow(ctx, `SELECT workspace_id,id,state,version,created_at,updated_at FROM execution.runs WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, string(target.WorkspaceID), string(target.RunID)))
	if err != nil {
		return application.AgentInputClaim{}, application.ErrAgentInputConflict
	}
	before := run.Snapshot()
	switch current.State {
	case "submitted":
		if current.AnswerSHA256 != prepared.AnswerSHA256 || current.SubmissionID != prepared.SubmissionID {
			return application.AgentInputClaim{}, application.ErrAgentInputConflict
		}
		if err = tx.Commit(ctx); err != nil {
			return application.AgentInputClaim{}, application.ErrAgentInputUnavailable
		}
		return application.AgentInputClaim{Target: current, Record: record, Replay: true}, nil
	case "sending", "unknown":
		if current.AnswerSHA256 == prepared.AnswerSHA256 && current.SubmissionID == prepared.SubmissionID {
			return application.AgentInputClaim{}, application.ErrAgentInputOutcomeUnknown
		}
		return application.AgentInputClaim{}, application.ErrAgentInputConflict
	case "pending":
		if before.State != domain.WaitingInput || current.AnswerSHA256 != "" || current.SubmissionID != "" || at.Before(before.UpdatedAt) {
			return application.AgentInputClaim{}, application.ErrAgentInputConflict
		}
	default:
		return application.AgentInputClaim{}, application.ErrAgentInputConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE execution.agent_input_requests SET state='sending',answer_sha256=$4,submission_id=$5,sending_at=$6,updated_at=$6 WHERE workspace_id=$1 AND run_id=$2 AND input_request_id=$3 AND state='pending' AND answer_sha256 IS NULL AND submission_id IS NULL`, string(target.WorkspaceID), string(target.RunID), target.InputRequestID, prepared.AnswerSHA256, prepared.SubmissionID, at)
	if err != nil || tag.RowsAffected() != 1 {
		return application.AgentInputClaim{}, application.ErrAgentInputConflict
	}
	current.State, current.AnswerSHA256, current.SubmissionID = "sending", prepared.AnswerSHA256, prepared.SubmissionID
	record.State, record.UpdatedAt = "sending", at
	if err = tx.Commit(ctx); err != nil {
		return application.AgentInputClaim{}, application.ErrAgentInputUnavailable
	}
	return application.AgentInputClaim{Target: current, Record: record}, nil
}

func saveAgentInputRun(ctx context.Context, tx pgx.Tx, before domain.Snapshot, run domain.Run, actor, reason string) error {
	after := run.Snapshot()
	if after.Version != before.Version+1 || after.WorkspaceID != before.WorkspaceID || after.ID != before.ID {
		return application.ErrAgentInputConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE execution.runs SET state=$1,version=$2,updated_at=$3 WHERE workspace_id=$4 AND id=$5 AND state=$6 AND version=$7 AND updated_at=$8`, string(after.State), int64(after.Version), after.UpdatedAt, string(after.WorkspaceID), string(after.ID), string(before.State), int64(before.Version), before.UpdatedAt)
	if err != nil || tag.RowsAffected() != 1 {
		return application.ErrAgentInputConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO execution.run_events(workspace_id,run_id,version,state,subject_id,credential_id,occurred_at,reason) VALUES($1,$2,$3,$4,$5,'agent_input',$6,$7)`, string(after.WorkspaceID), string(after.ID), int64(after.Version), string(after.State), actor, after.UpdatedAt, reason); err != nil {
		return application.ErrAgentInputUnavailable
	}
	return nil
}

func (r *AgentInputs) RecordAgentInputAccepted(ctx context.Context, target application.AgentInputSubmissionTarget, prepared application.PreparedAgentInput, at time.Time) (application.AgentInputSubmissionRecord, error) {
	tx, err := r.scoped(ctx, target.WorkspaceID)
	if err != nil {
		return application.AgentInputSubmissionRecord{}, err
	}
	defer rollback(tx)
	current, record, err := loadLockedAgentInput(ctx, tx, target)
	if err != nil {
		return application.AgentInputSubmissionRecord{}, err
	}
	if current.AnswerSHA256 != prepared.AnswerSHA256 || current.SubmissionID != prepared.SubmissionID {
		return application.AgentInputSubmissionRecord{}, application.ErrAgentInputConflict
	}
	if current.State == "submitted" {
		if err = tx.Commit(ctx); err != nil {
			return application.AgentInputSubmissionRecord{}, application.ErrAgentInputUnavailable
		}
		return record, nil
	}
	if current.State != "sending" {
		return application.AgentInputSubmissionRecord{}, application.ErrAgentInputConflict
	}
	run, err := scanRun(tx.QueryRow(ctx, `SELECT workspace_id,id,state,version,created_at,updated_at FROM execution.runs WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, string(target.WorkspaceID), string(target.RunID)))
	if err != nil {
		return application.AgentInputSubmissionRecord{}, application.ErrAgentInputUnavailable
	}
	before := run.Snapshot()
	if at.Before(before.UpdatedAt) {
		at = before.UpdatedAt
	}
	if before.State == domain.WaitingInput {
		if err = run.Resume(at); err != nil {
			return application.AgentInputSubmissionRecord{}, application.ErrAgentInputConflict
		}
		if err = saveAgentInputRun(ctx, tx, before, run, "agent_input_sender", "provider accepted supplemental input"); err != nil {
			return application.AgentInputSubmissionRecord{}, err
		}
	} else if before.State != domain.CancelRequested && before.State != domain.Reconciling && !before.State.IsTerminal() {
		return application.AgentInputSubmissionRecord{}, application.ErrAgentInputConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE execution.agent_input_requests SET state='submitted',submitted_at=$4,updated_at=$4 WHERE workspace_id=$1 AND run_id=$2 AND input_request_id=$3 AND state='sending' AND answer_sha256=$5 AND submission_id=$6`, string(target.WorkspaceID), string(target.RunID), target.InputRequestID, at, prepared.AnswerSHA256, prepared.SubmissionID)
	if err != nil || tag.RowsAffected() != 1 {
		return application.AgentInputSubmissionRecord{}, application.ErrAgentInputConflict
	}
	record.State, record.UpdatedAt = "submitted", at
	if err = tx.Commit(ctx); err != nil {
		return application.AgentInputSubmissionRecord{}, application.ErrAgentInputUnavailable
	}
	return record, nil
}

func (r *AgentInputs) RecordAgentInputUnknown(ctx context.Context, target application.AgentInputSubmissionTarget, prepared application.PreparedAgentInput, at time.Time) (application.AgentInputSubmissionRecord, error) {
	tx, err := r.scoped(ctx, target.WorkspaceID)
	if err != nil {
		return application.AgentInputSubmissionRecord{}, err
	}
	defer rollback(tx)
	current, record, err := loadLockedAgentInput(ctx, tx, target)
	if err != nil {
		return application.AgentInputSubmissionRecord{}, err
	}
	if current.AnswerSHA256 != prepared.AnswerSHA256 || current.SubmissionID != prepared.SubmissionID {
		return application.AgentInputSubmissionRecord{}, application.ErrAgentInputConflict
	}
	if current.State == "unknown" {
		if err = tx.Commit(ctx); err != nil {
			return application.AgentInputSubmissionRecord{}, application.ErrAgentInputUnavailable
		}
		return record, nil
	}
	if current.State != "sending" {
		return application.AgentInputSubmissionRecord{}, application.ErrAgentInputConflict
	}
	run, err := scanRun(tx.QueryRow(ctx, `SELECT workspace_id,id,state,version,created_at,updated_at FROM execution.runs WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, string(target.WorkspaceID), string(target.RunID)))
	if err != nil {
		return application.AgentInputSubmissionRecord{}, application.ErrAgentInputUnavailable
	}
	before := run.Snapshot()
	if at.Before(before.UpdatedAt) {
		at = before.UpdatedAt
	}
	if before.State == domain.WaitingInput {
		if err = run.MarkOutcomeUnconfirmed(at); err != nil {
			return application.AgentInputSubmissionRecord{}, application.ErrAgentInputConflict
		}
		if err = saveAgentInputRun(ctx, tx, before, run, "agent_input_sender", "supplemental input delivery outcome unknown"); err != nil {
			return application.AgentInputSubmissionRecord{}, err
		}
	} else if before.State != domain.CancelRequested && before.State != domain.Reconciling && !before.State.IsTerminal() {
		return application.AgentInputSubmissionRecord{}, application.ErrAgentInputConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE execution.agent_input_requests SET state='unknown',updated_at=$4 WHERE workspace_id=$1 AND run_id=$2 AND input_request_id=$3 AND state='sending' AND answer_sha256=$5 AND submission_id=$6`, string(target.WorkspaceID), string(target.RunID), target.InputRequestID, at, prepared.AnswerSHA256, prepared.SubmissionID)
	if err != nil || tag.RowsAffected() != 1 {
		return application.AgentInputSubmissionRecord{}, application.ErrAgentInputConflict
	}
	record.State, record.UpdatedAt = "unknown", at
	if err = tx.Commit(ctx); err != nil {
		return application.AgentInputSubmissionRecord{}, application.ErrAgentInputUnavailable
	}
	return record, nil
}

var _ application.AgentInputSubmissionRepository = (*AgentInputs)(nil)
