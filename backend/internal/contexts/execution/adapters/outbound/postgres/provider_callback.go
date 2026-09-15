package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

type ProviderCallbacks struct{ pool *pgxpool.Pool }

func NewProviderCallbacks(pool *pgxpool.Pool) *ProviderCallbacks {
	return &ProviderCallbacks{pool: pool}
}

func callbackEventLockKey(callback application.ProviderCallback) int64 {
	digest := sha256.Sum256([]byte("mender-provider-callback-lock-v1\x00" + callback.ProviderID + "\x00" + callback.EventID))
	return int64(binary.BigEndian.Uint64(digest[:8]))
}

type callbackInboxRow struct {
	ReceiptID, ProviderID, EventID, BodySHA256, WorkspaceID, RunID, ObservationID string
	Disposition, ReasonCode                                                       string
	ReceivedAt, LastReceivedAt, ProcessedAt                                       time.Time
	DeliveryCount                                                                 int
}

func callbackReceiptID(callback application.ProviderCallback) string {
	digest := sha256.Sum256([]byte("mender-provider-callback-v1\x00" + callback.WorkspaceID + "\x00" + callback.ProviderID + "\x00" + callback.EventID + "\x00" + callback.BodySHA256))
	return hex.EncodeToString(digest[:])
}

func callbackReceipt(row callbackInboxRow, disposition application.ProviderCallbackDisposition) application.ProviderCallbackReceipt {
	return application.ProviderCallbackReceipt{
		ReceiptID: row.ReceiptID, ProviderID: row.ProviderID, EventID: row.EventID,
		WorkspaceID: row.WorkspaceID, RunID: row.RunID, ObservationID: row.ObservationID,
		Disposition: disposition, ReasonCode: row.ReasonCode, ReceivedAt: row.ReceivedAt, ProcessedAt: row.ProcessedAt,
	}
}

func scanCallbackInbox(row pgx.Row) (callbackInboxRow, error) {
	var value callbackInboxRow
	var reason *string
	var processed *time.Time
	err := row.Scan(&value.ReceiptID, &value.ProviderID, &value.EventID, &value.BodySHA256, &value.WorkspaceID, &value.RunID, &value.ObservationID,
		&value.Disposition, &reason, &value.ReceivedAt, &value.LastReceivedAt, &value.DeliveryCount, &processed)
	if reason != nil {
		value.ReasonCode = *reason
	}
	if processed != nil {
		value.ProcessedAt = processed.UTC()
	}
	value.ReceivedAt = value.ReceivedAt.UTC()
	value.LastReceivedAt = value.LastReceivedAt.UTC()
	return value, err
}

func callbackInboxColumns() string {
	return `receipt_id,provider_id,event_id,body_sha256,workspace_id,run_id,observation_id,disposition,reason_code,received_at,last_received_at,delivery_count,processed_at`
}

func (r *ProviderCallbacks) scoped(ctx context.Context, workspace string) (pgx.Tx, error) {
	if r == nil || r.pool == nil || !domain.WorkspaceID(workspace).IsValid() {
		return nil, application.ErrProviderCallbackUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrProviderCallbackUnavailable
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace); err != nil {
		rollback(tx)
		return nil, application.ErrProviderCallbackUnavailable
	}
	return tx, nil
}

func callbackObservation(callback application.ProviderCallback) domain.ProviderObservation {
	return domain.ProviderObservation{
		WorkspaceID: domain.WorkspaceID(callback.WorkspaceID), RunID: domain.RunID(callback.RunID),
		ObservationID: callback.ObservationID, AttemptNo: callback.AttemptNo, ProviderID: callback.ProviderID,
		ProviderRequestID: callback.ProviderRequestID, ExternalTaskID: callback.ExternalTaskID,
		State: domain.ProviderResultState(callback.State), ResultJSON: callback.ResultJSON, ErrorCode: callback.ErrorCode,
		ObservedAt: callback.ObservedAt,
	}
}

func markCallback(ctx context.Context, tx pgx.Tx, callback application.ProviderCallback, receiptID, disposition, reason string) error {
	tag, err := tx.Exec(ctx, `UPDATE execution.provider_callback_inbox
	 SET disposition=$1,reason_code=NULLIF($2,''),processed_at=$3
	 WHERE workspace_id=$4 AND receipt_id=$5 AND disposition='pending'`, disposition, reason, callback.ReceivedAt, callback.WorkspaceID, receiptID)
	if err != nil || tag.RowsAffected() != 1 {
		return application.ErrProviderCallbackUnavailable
	}
	return nil
}

func quarantineCallback(ctx context.Context, tx pgx.Tx, callback application.ProviderCallback, receiptID, reason string) (application.ProviderCallbackReceipt, error) {
	if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT callback_convergence`); err != nil {
		return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackUnavailable
	}
	if err := markCallback(ctx, tx, callback, receiptID, "quarantined", reason); err != nil {
		return application.ProviderCallbackReceipt{}, err
	}
	if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT callback_convergence`); err != nil {
		return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackUnavailable
	}
	return application.ProviderCallbackReceipt{
		ReceiptID: receiptID, ProviderID: callback.ProviderID, EventID: callback.EventID,
		WorkspaceID: callback.WorkspaceID, RunID: callback.RunID, ObservationID: callback.ObservationID,
		Disposition: application.ProviderCallbackQuarantined, ReasonCode: reason,
		ReceivedAt: callback.ReceivedAt, ProcessedAt: callback.ReceivedAt,
	}, nil
}

func keepCallbackPending(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT callback_convergence`); err != nil {
		return application.ErrProviderCallbackUnavailable
	}
	if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT callback_convergence`); err != nil {
		return application.ErrProviderCallbackUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return application.ErrProviderCallbackUnavailable
	}
	return application.ErrProviderCallbackUnavailable
}

func precheckCallback(ctx context.Context, tx pgx.Tx, callback application.ProviderCallback) (string, error) {
	attempt, err := scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptColumns+` FROM execution.run_attempts a WHERE a.workspace_id=$1 AND a.run_id=$2 AND a.attempt_no=$3`, callback.WorkspaceID, callback.RunID, int32(callback.AttemptNo)))
	if errors.Is(err, pgx.ErrNoRows) {
		return "attempt_binding_mismatch", nil
	}
	if err != nil {
		return "", application.ErrProviderCallbackUnavailable
	}
	if (attempt.State != domain.AttemptSubmitted && attempt.State != domain.AttemptUnknown) || attempt.ProviderID != callback.ProviderID || attempt.ProviderRequestID != callback.ProviderRequestID || callback.ExternalTaskID != "" && attempt.ExternalTaskID != callback.ExternalTaskID {
		return "attempt_binding_mismatch", nil
	}
	evidenceAt := attempt.SubmissionIntentAt
	if !attempt.SubmittedAt.IsZero() {
		evidenceAt = attempt.SubmittedAt
	}
	if callback.ObservedAt.Before(evidenceAt) {
		return "observation_before_submission", nil
	}
	var latest *time.Time
	if err = tx.QueryRow(ctx, `SELECT max(observed_at) FROM execution.provider_observations WHERE workspace_id=$1 AND run_id=$2`, callback.WorkspaceID, callback.RunID).Scan(&latest); err != nil {
		return "", application.ErrProviderCallbackUnavailable
	}
	if latest != nil && callback.ObservedAt.Before(latest.UTC()) {
		return "out_of_order", nil
	}
	var runState, jobState string
	if err = tx.QueryRow(ctx, `SELECT r.state,j.state FROM execution.runs r JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(r.workspace_id,r.id) WHERE r.workspace_id=$1 AND r.id=$2`, callback.WorkspaceID, callback.RunID).Scan(&runState, &jobState); errors.Is(err, pgx.ErrNoRows) {
		return "attempt_binding_mismatch", nil
	} else if err != nil {
		return "", application.ErrProviderCallbackUnavailable
	}
	if domain.State(runState).IsTerminal() || domain.JobState(jobState) == domain.JobFinished {
		return "terminal_replay", nil
	}
	return "", nil
}

func (r *ProviderCallbacks) IngestProviderCallback(ctx context.Context, callback application.ProviderCallback) (application.ProviderCallbackReceipt, error) {
	if err := ctx.Err(); err != nil {
		return application.ProviderCallbackReceipt{}, err
	}
	var observation domain.ProviderObservation
	var inputRequest application.AgentInputRequest
	switch callback.EventType {
	case "provider.observation":
		observation = callbackObservation(callback)
		if observation.Validate() != nil {
			return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackInvalid
		}
	case "provider.input_required":
		inputRequest = application.AgentInputRequest{
			WorkspaceID: domain.WorkspaceID(callback.WorkspaceID), RunID: domain.RunID(callback.RunID), AttemptNo: callback.AttemptNo,
			ProviderID: callback.ProviderID, ProviderRequestID: callback.ProviderRequestID, ExternalTaskID: callback.ExternalTaskID,
			InputRequestID: callback.InputRequestID, Prompt: callback.InputPrompt, InputSchemaJSON: callback.InputSchemaJSON, RequestedAt: callback.ObservedAt,
		}
		if inputRequest.Validate() != nil {
			return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackInvalid
		}
	default:
		return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackInvalid
	}
	tx, err := r.scoped(ctx, callback.WorkspaceID)
	if err != nil {
		return application.ProviderCallbackReceipt{}, err
	}
	defer rollback(tx)

	// Serialize a provider event identity before checking exact-body duplicates or
	// conflicting deliveries. This is local metadata locking only; execution
	// ownership is still established by the Attempt binding below.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, callbackEventLockKey(callback)); err != nil {
		return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackUnavailable
	}

	existing, err := scanCallbackInbox(tx.QueryRow(ctx, `SELECT `+callbackInboxColumns()+` FROM execution.provider_callback_inbox WHERE workspace_id=$1 AND provider_id=$2 AND event_id=$3 AND body_sha256=$4 FOR UPDATE`, callback.WorkspaceID, callback.ProviderID, callback.EventID, callback.BodySHA256))
	if err == nil {
		if _, err = tx.Exec(ctx, `UPDATE execution.provider_callback_inbox SET delivery_count=delivery_count+1,last_received_at=GREATEST(last_received_at,$1) WHERE workspace_id=$2 AND receipt_id=$3`, callback.ReceivedAt, callback.WorkspaceID, existing.ReceiptID); err != nil {
			return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackUnavailable
		}
		if existing.Disposition != "pending" {
			if err = tx.Commit(ctx); err != nil {
				return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackUnavailable
			}
			return callbackReceipt(existing, application.ProviderCallbackDuplicate), nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackUnavailable
	}

	if errors.Is(err, pgx.ErrNoRows) {
		var conflict bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution.provider_callback_inbox WHERE workspace_id=$1 AND provider_id=$2 AND event_id=$3 AND body_sha256<>$4)`, callback.WorkspaceID, callback.ProviderID, callback.EventID, callback.BodySHA256).Scan(&conflict); err != nil {
			return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackUnavailable
		}
		receiptID := callbackReceiptID(callback)
		disposition, reason, processed := "pending", "", any(nil)
		if conflict {
			disposition, reason, processed = "quarantined", "event_id_conflict", callback.ReceivedAt
		}
		_, err = tx.Exec(ctx, `INSERT INTO execution.provider_callback_inbox(receipt_id,workspace_id,provider_id,event_id,body_sha256,key_id,signed_at,received_at,last_received_at,delivery_count,run_id,attempt_no,provider_request_id,external_task_id,observation_id,observation_state,observed_at,disposition,reason_code,processed_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8,1,$9,$10,$11,NULLIF($12,''),$13,$14,$15,$16,NULLIF($17,''),$18)`,
			receiptID, callback.WorkspaceID, callback.ProviderID, callback.EventID, callback.BodySHA256, callback.KeyID,
			callback.SignedAt, callback.ReceivedAt, callback.RunID, int32(callback.AttemptNo), callback.ProviderRequestID,
			callback.ExternalTaskID, callback.ObservationID, callback.State, callback.ObservedAt, disposition, reason, processed)
		if err != nil {
			return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackUnavailable
		}
		if conflict {
			if err = tx.Commit(ctx); err != nil {
				return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackUnavailable
			}
			return application.ProviderCallbackReceipt{
				ReceiptID: receiptID, ProviderID: callback.ProviderID, EventID: callback.EventID,
				WorkspaceID: callback.WorkspaceID, RunID: callback.RunID, ObservationID: callback.ObservationID,
				Disposition: application.ProviderCallbackQuarantined, ReasonCode: "event_id_conflict",
				ReceivedAt: callback.ReceivedAt, ProcessedAt: callback.ReceivedAt,
			}, nil
		}
		existing.ReceiptID = receiptID
	}

	if _, err = tx.Exec(ctx, `SAVEPOINT callback_convergence`); err != nil {
		return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackUnavailable
	}
	if reason, checkErr := precheckCallback(ctx, tx, callback); checkErr != nil {
		if errors.Is(checkErr, context.Canceled) || errors.Is(checkErr, context.DeadlineExceeded) {
			return application.ProviderCallbackReceipt{}, checkErr
		}
		return application.ProviderCallbackReceipt{}, keepCallbackPending(ctx, tx)
	} else if reason != "" {
		return quarantineCallback(ctx, tx, callback, existing.ReceiptID, reason)
	}

	if callback.EventType == "provider.input_required" {
		if _, err = recordAgentInputRequestTx(ctx, tx, inputRequest); err != nil {
			switch {
			case errors.Is(err, application.ErrAgentInputConflict), errors.Is(err, application.ErrAgentInputInvalid):
				return quarantineCallback(ctx, tx, callback, existing.ReceiptID, "execution_conflict")
			case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
				return application.ProviderCallbackReceipt{}, err
			default:
				return application.ProviderCallbackReceipt{}, keepCallbackPending(ctx, tx)
			}
		}
	} else {
		if _, err = recordProviderObservationTx(ctx, tx, observation); err != nil {
			switch {
			case errors.Is(err, application.ErrProviderAlreadyTerminal):
				return quarantineCallback(ctx, tx, callback, existing.ReceiptID, "terminal_replay")
			case errors.Is(err, application.ErrProviderResultConflict), errors.Is(err, domain.ErrInvalidProviderObservation):
				return quarantineCallback(ctx, tx, callback, existing.ReceiptID, "execution_conflict")
			case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
				return application.ProviderCallbackReceipt{}, err
			default:
				return application.ProviderCallbackReceipt{}, keepCallbackPending(ctx, tx)
			}
		}
	}
	if err = markCallback(ctx, tx, callback, existing.ReceiptID, "accepted", ""); err != nil {
		return application.ProviderCallbackReceipt{}, err
	}
	if _, err = tx.Exec(ctx, `RELEASE SAVEPOINT callback_convergence`); err != nil {
		return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ProviderCallbackReceipt{}, application.ErrProviderCallbackUnavailable
	}
	return application.ProviderCallbackReceipt{
		ReceiptID: existing.ReceiptID, ProviderID: callback.ProviderID, EventID: callback.EventID,
		WorkspaceID: callback.WorkspaceID, RunID: callback.RunID, ObservationID: callback.ObservationID,
		Disposition: application.ProviderCallbackAccepted, ReceivedAt: callback.ReceivedAt, ProcessedAt: callback.ReceivedAt,
	}, nil
}

var _ application.ProviderCallbackRepository = (*ProviderCallbacks)(nil)
