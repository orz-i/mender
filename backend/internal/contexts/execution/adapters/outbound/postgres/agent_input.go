package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

func scanAgentInputRequest(row rowScanner) (application.AgentInputRequestRecord, error) {
	var item application.AgentInputRequestRecord
	return item, row.Scan(&item.WorkspaceID, &item.RunID, &item.InputRequestID, &item.State, &item.Prompt, &item.InputSchemaJSON, &item.RequestedAt, &item.UpdatedAt)
}

func recordAgentInputRequestTx(ctx context.Context, tx pgx.Tx, request application.AgentInputRequest) (application.AgentInputRequestRecord, error) {
	run, err := scanRun(tx.QueryRow(ctx, `SELECT workspace_id,id,state,version,created_at,updated_at FROM execution.runs WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, string(request.WorkspaceID), string(request.RunID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return application.AgentInputRequestRecord{}, application.ErrAgentInputConflict
	}
	if err != nil {
		return application.AgentInputRequestRecord{}, application.ErrAgentInputUnavailable
	}
	before := run.Snapshot()
	if before.State != domain.Running && before.State != domain.WaitingInput {
		return application.AgentInputRequestRecord{}, application.ErrAgentInputConflict
	}
	var attemptState, providerID, providerRequestID string
	var externalTask *string
	err = tx.QueryRow(ctx, `SELECT state,provider_id,provider_request_id,external_task_id FROM execution.run_attempts WHERE workspace_id=$1 AND run_id=$2 AND attempt_no=$3`, string(request.WorkspaceID), string(request.RunID), int32(request.AttemptNo)).Scan(&attemptState, &providerID, &providerRequestID, &externalTask)
	if err != nil || (attemptState != string(domain.AttemptSubmitted) && attemptState != string(domain.AttemptUnknown)) || providerID != request.ProviderID || providerRequestID != request.ProviderRequestID || externalTask == nil || *externalTask != request.ExternalTaskID {
		return application.AgentInputRequestRecord{}, application.ErrAgentInputConflict
	}

	existing, err := scanAgentInputRequest(tx.QueryRow(ctx, `SELECT workspace_id,run_id,input_request_id,state,prompt,input_schema_json::text,requested_at,updated_at FROM execution.agent_input_requests WHERE workspace_id=$1 AND run_id=$2 AND input_request_id=$3`, string(request.WorkspaceID), string(request.RunID), request.InputRequestID))
	if err == nil {
		var same bool
		if err = tx.QueryRow(ctx, `SELECT attempt_no=$4 AND provider_id=$5 AND provider_request_id=$6 AND external_task_id=$7 AND prompt=$8 AND input_schema_json=$9::jsonb AND requested_at=$10 FROM execution.agent_input_requests WHERE workspace_id=$1 AND run_id=$2 AND input_request_id=$3`, string(request.WorkspaceID), string(request.RunID), request.InputRequestID, int32(request.AttemptNo), request.ProviderID, request.ProviderRequestID, request.ExternalTaskID, request.Prompt, request.InputSchemaJSON, request.RequestedAt).Scan(&same); err != nil || !same {
			return application.AgentInputRequestRecord{}, application.ErrAgentInputConflict
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return application.AgentInputRequestRecord{}, application.ErrAgentInputUnavailable
	}
	var existingRequest bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution.agent_input_requests WHERE workspace_id=$1 AND run_id=$2)`, string(request.WorkspaceID), string(request.RunID)).Scan(&existingRequest); err != nil {
		return application.AgentInputRequestRecord{}, application.ErrAgentInputUnavailable
	}
	if existingRequest {
		return application.AgentInputRequestRecord{}, application.ErrAgentInputConflict
	}
	if before.State == domain.Running {
		at := request.RequestedAt.UTC().Truncate(time.Microsecond)
		if at.Before(before.UpdatedAt) {
			return application.AgentInputRequestRecord{}, application.ErrAgentInputConflict
		}
		if err = run.WaitForInput(at); err != nil {
			return application.AgentInputRequestRecord{}, application.ErrAgentInputConflict
		}
		after := run.Snapshot()
		tag, updateErr := tx.Exec(ctx, `UPDATE execution.runs SET state=$1,version=$2,updated_at=$3 WHERE workspace_id=$4 AND id=$5 AND state=$6 AND version=$7 AND updated_at=$8`, string(after.State), int64(after.Version), after.UpdatedAt, string(after.WorkspaceID), string(after.ID), string(before.State), int64(before.Version), before.UpdatedAt)
		if updateErr != nil || tag.RowsAffected() != 1 {
			return application.AgentInputRequestRecord{}, application.ErrAgentInputConflict
		}
		if _, err = tx.Exec(ctx, `INSERT INTO execution.run_events(workspace_id,run_id,version,state,subject_id,credential_id,occurred_at,reason) VALUES($1,$2,$3,'waiting_input','provider_reconciler','agent_input_request',$4,$5)`, string(after.WorkspaceID), string(after.ID), int64(after.Version), after.UpdatedAt, "provider requested supplemental input"); err != nil {
			return application.AgentInputRequestRecord{}, application.ErrAgentInputUnavailable
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO execution.agent_input_requests(workspace_id,run_id,attempt_no,input_request_id,provider_id,provider_request_id,external_task_id,prompt,input_schema_json,state,requested_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,'pending',$10,$10)`, string(request.WorkspaceID), string(request.RunID), int32(request.AttemptNo), request.InputRequestID, request.ProviderID, request.ProviderRequestID, request.ExternalTaskID, request.Prompt, request.InputSchemaJSON, request.RequestedAt)
	if err != nil {
		return application.AgentInputRequestRecord{}, application.ErrAgentInputUnavailable
	}
	item, err := scanAgentInputRequest(tx.QueryRow(ctx, `SELECT workspace_id,run_id,input_request_id,state,prompt,input_schema_json::text,requested_at,updated_at FROM execution.agent_input_requests WHERE workspace_id=$1 AND run_id=$2 AND input_request_id=$3`, string(request.WorkspaceID), string(request.RunID), request.InputRequestID))
	if err != nil {
		return application.AgentInputRequestRecord{}, application.ErrAgentInputUnavailable
	}
	return item, nil
}

func (r *ProviderResults) RecordAgentInputRequest(ctx context.Context, request application.AgentInputRequest) (application.AgentInputRequestRecord, error) {
	if request.Validate() != nil {
		return application.AgentInputRequestRecord{}, application.ErrAgentInputInvalid
	}
	tx, err := r.scoped(ctx, request.WorkspaceID)
	if err != nil {
		return application.AgentInputRequestRecord{}, application.ErrAgentInputUnavailable
	}
	defer rollback(tx)
	item, err := recordAgentInputRequestTx(ctx, tx, request)
	if err != nil {
		return application.AgentInputRequestRecord{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.AgentInputRequestRecord{}, application.ErrAgentInputUnavailable
	}
	return item, nil
}

var _ application.AgentInputRequestRepository = (*ProviderResults)(nil)
