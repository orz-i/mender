package httpexecutor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type ControlBroker interface {
	PrepareControl(context.Context, application.InvocationRef, time.Time) (application.PreparedControl, error)
}

func decodeInputRequest(raw json.RawMessage) (id, prompt, schema string, ok bool) {
	if len(raw) < 2 || len(raw) > 70<<10 {
		return "", "", "", false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 3 || fields["input_request_id"] == nil || fields["prompt"] == nil || fields["input_schema"] == nil {
		return "", "", "", false
	}
	var requestID, inputPrompt string
	if json.Unmarshal(fields["input_request_id"], &requestID) != nil || json.Unmarshal(fields["prompt"], &inputPrompt) != nil || !validObservationID(requestID) || len(inputPrompt) < 1 || len(inputPrompt) > 2000 || len(fields["input_schema"]) < 2 || len(fields["input_schema"]) > 64<<10 || !json.Valid(fields["input_schema"]) || bytes.Equal(bytes.TrimSpace(fields["input_schema"]), []byte("null")) {
		return "", "", "", false
	}
	var object map[string]any
	if json.Unmarshal(fields["input_schema"], &object) != nil || object == nil {
		return "", "", "", false
	}
	canonical, err := json.Marshal(object)
	if err != nil || len(canonical) > 64<<10 {
		return "", "", "", false
	}
	return requestID, inputPrompt, string(canonical), true
}

func validControlHandle(value string, required bool) bool {
	if value == "" {
		return !required
	}
	if len(value) > 512 || !utf8.ValidString(value) {
		return false
	}
	for _, ch := range value {
		if ch < 0x20 || ch == 0x7f {
			return false
		}
	}
	return true
}

func validControlQuery(workspace, run string, attempt uint32, provider, request, task string) bool {
	return workspace != "" && run != "" && attempt > 0 && attempt <= 100 && validProviderValue(provider, true) && validControlHandle(request, true) && validControlHandle(task, false)
}

func validCancelKey(value string) bool {
	if len(value) < 8 || len(value) > 200 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == ':' || ch == '-') {
			return false
		}
	}
	return true
}

func validObservationID(value string) bool {
	if len(value) < 1 || len(value) > 200 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == ':' || ch == '-') {
			return false
		}
	}
	return true
}

func validErrorCode(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == ':' || ch == '-') {
			return false
		}
	}
	return true
}

func (e *Executor) prepareControl(ctx context.Context, workspace, run, provider string) (application.PreparedControl, error) {
	broker, ok := e.broker.(ControlBroker)
	if !ok || broker == nil {
		return application.PreparedControl{}, supply.ErrExecutorUnavailable
	}
	at := e.clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() || at.Year() < 1 || at.Year() > 9999 {
		return application.PreparedControl{}, supply.ErrExecutorUnavailable
	}
	prepared, err := broker.PrepareControl(ctx, application.InvocationRef{WorkspaceID: workspace, RunID: run}, at)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return application.PreparedControl{}, err
		}
		return application.PreparedControl{}, supply.ErrExecutorUnavailable
	}
	if prepared.WorkspaceID != workspace || prepared.RunID != run || prepared.Deployment.Validate() != nil || prepared.Deployment.ProviderID != provider || !e.transportAllowed(prepared.Deployment.TransportKind) {
		return application.PreparedControl{}, supply.ErrExecutorUnavailable
	}
	return prepared, nil
}

// doControlJSON reuses the exact hardened resolver/dialer/client path used by
// Submit. uncertain=true means the caller cannot prove whether a side-effecting
// request reached the provider; cancellation must never automatically reissue.
func (e *Executor) doControlJSON(ctx context.Context, prepared application.PreparedControl, endpoint string, body []byte, idempotencyKey string) ([]byte, bool, error) {
	if len(body) == 0 || len(body) > prepared.Deployment.MaxRequestBytes {
		return nil, false, supply.ErrExecutorForbidden
	}
	requestCtx, cancel := context.WithTimeout(ctx, prepared.Deployment.RequestTimeout)
	defer cancel()
	resolved, pinnedAddress, err := e.resolveEndpoint(requestCtx, endpoint)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		if requestCtx.Err() != nil {
			return nil, true, nil
		}
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, resolved.String(), bytes.NewReader(body))
	if err != nil {
		return nil, false, supply.ErrExecutorUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if idempotencyKey != "" {
		if !validCancelKey(idempotencyKey) || !safeHeaderName(prepared.Deployment.IdempotencyHeader) || reservedHeader(prepared.Deployment.IdempotencyHeader) {
			return nil, false, supply.ErrExecutorForbidden
		}
		req.Header.Set(prepared.Deployment.IdempotencyHeader, idempotencyKey)
	}
	if err = buildAuthHeaders(req, prepared.Deployment, prepared.Secret); err != nil {
		return nil, false, err
	}
	client, closeClient := e.newClient(prepared.Deployment, pinnedAddress)
	defer closeClient()
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, true, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, true, nil
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, true, nil
	}
	limit := int64(prepared.Deployment.MaxResponseBytes)
	result, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(result)) > limit {
		return nil, true, nil
	}
	return result, false, nil
}

func decodeObservedAt(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false
	}
	parsed = parsed.UTC().Truncate(time.Microsecond)
	return parsed, !parsed.IsZero() && parsed.Year() >= 1 && parsed.Year() <= 9999
}

func decodeStatus(body []byte) (supply.StatusObservation, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return supply.StatusObservation{}, errors.New("invalid provider status response")
	}
	seen := map[string]bool{}
	var observation supply.StatusObservation
	var state, observedAt string
	for decoder.More() {
		keyToken, tokenErr := decoder.Token()
		key, ok := keyToken.(string)
		if tokenErr != nil || !ok || seen[key] {
			return supply.StatusObservation{}, errors.New("invalid provider status response")
		}
		seen[key] = true
		switch key {
		case "observation_id":
			if err = decoder.Decode(&observation.ObservationID); err != nil {
				return supply.StatusObservation{}, errors.New("invalid provider status response")
			}
		case "state":
			if err = decoder.Decode(&state); err != nil {
				return supply.StatusObservation{}, errors.New("invalid provider status response")
			}
		case "result":
			var raw json.RawMessage
			if err = decoder.Decode(&raw); err != nil || len(raw) == 0 || len(raw) > 1<<20 || !json.Valid(raw) || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return supply.StatusObservation{}, errors.New("invalid provider status response")
			}
			observation.ResultJSON = string(raw)
		case "error_code":
			if err = decoder.Decode(&observation.ErrorCode); err != nil {
				return supply.StatusObservation{}, errors.New("invalid provider status response")
			}
		case "observed_at":
			if err = decoder.Decode(&observedAt); err != nil {
				return supply.StatusObservation{}, errors.New("invalid provider status response")
			}
		case "input_request":
			var raw json.RawMessage
			if err = decoder.Decode(&raw); err != nil {
				return supply.StatusObservation{}, errors.New("invalid provider status response")
			}
			var ok bool
			observation.InputRequestID, observation.InputPrompt, observation.InputSchemaJSON, ok = decodeInputRequest(raw)
			if !ok {
				return supply.StatusObservation{}, errors.New("invalid provider status response")
			}
		default:
			return supply.StatusObservation{}, errors.New("invalid provider status response")
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return supply.StatusObservation{}, errors.New("invalid provider status response")
	}
	if token, err = decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return supply.StatusObservation{}, errors.New("invalid provider status response")
	}
	if !validObservationID(observation.ObservationID) {
		return supply.StatusObservation{}, errors.New("invalid provider status response")
	}
	parsedAt, ok := decodeObservedAt(observedAt)
	if !ok {
		return supply.StatusObservation{}, errors.New("invalid provider status response")
	}
	observation.ObservedAt = parsedAt
	switch supply.ProviderStatusState(state) {
	case supply.StatusPending:
		if observation.ResultJSON != "" || observation.ErrorCode != "" || observation.InputRequestID != "" {
			return supply.StatusObservation{}, errors.New("invalid provider status response")
		}
		observation.State = supply.StatusPending
	case supply.StatusInputRequired:
		if observation.ResultJSON != "" || observation.ErrorCode != "" || observation.InputRequestID == "" || observation.InputPrompt == "" || observation.InputSchemaJSON == "" {
			return supply.StatusObservation{}, errors.New("invalid provider status response")
		}
		observation.State = supply.StatusInputRequired
	case supply.StatusSucceeded:
		if observation.ResultJSON == "" || observation.ErrorCode != "" || observation.InputRequestID != "" {
			return supply.StatusObservation{}, errors.New("invalid provider status response")
		}
		observation.State = supply.StatusSucceeded
	case supply.StatusFailed:
		if observation.ResultJSON != "" || !validErrorCode(observation.ErrorCode) || observation.InputRequestID != "" {
			return supply.StatusObservation{}, errors.New("invalid provider status response")
		}
		observation.State = supply.StatusFailed
	case supply.StatusCanceled:
		if observation.ResultJSON != "" || observation.ErrorCode != "" || observation.InputRequestID != "" {
			return supply.StatusObservation{}, errors.New("invalid provider status response")
		}
		observation.State = supply.StatusCanceled
	default:
		return supply.StatusObservation{}, errors.New("invalid provider status response")
	}
	return observation, nil
}

func (e *Executor) QueryStatus(ctx context.Context, query supply.StatusQuery) (supply.StatusObservation, error) {
	if err := ctx.Err(); err != nil {
		return supply.StatusObservation{}, err
	}
	if !validControlQuery(query.WorkspaceID, query.RunID, query.AttemptNo, query.ProviderID, query.ProviderRequestID, query.ExternalTaskID) {
		return supply.StatusObservation{}, supply.ErrProviderStatusUnavailable
	}
	prepared, err := e.prepareControl(ctx, query.WorkspaceID, query.RunID, query.ProviderID)
	if err != nil || !prepared.Deployment.SupportsStatusQuery() {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return supply.StatusObservation{}, err
		}
		return supply.StatusObservation{}, supply.ErrProviderStatusUnavailable
	}
	body, err := json.Marshal(struct {
		ProviderRequestID string `json:"provider_request_id"`
		ExternalTaskID    string `json:"external_task_id,omitempty"`
	}{query.ProviderRequestID, query.ExternalTaskID})
	if err != nil {
		return supply.StatusObservation{}, supply.ErrProviderStatusUnavailable
	}
	response, uncertain, err := e.doControlJSON(ctx, prepared, prepared.Deployment.StatusEndpointURL, body, "")
	if err != nil || uncertain {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return supply.StatusObservation{}, err
		}
		return supply.StatusObservation{}, supply.ErrProviderStatusUnavailable
	}
	observation, err := decodeStatus(response)
	if err != nil {
		return supply.StatusObservation{}, supply.ErrProviderStatusUnavailable
	}
	if observation.State == supply.StatusInputRequired && !prepared.Deployment.SupportsSupplementalInput() {
		return supply.StatusObservation{}, supply.ErrProviderStatusUnavailable
	}
	return observation, nil
}

func decodeCancel(body []byte) (supply.CancelResult, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return supply.CancelResult{}, errors.New("invalid provider cancel response")
	}
	seen := map[string]bool{}
	var disposition, observationID, observedAt string
	for decoder.More() {
		keyToken, tokenErr := decoder.Token()
		key, ok := keyToken.(string)
		if tokenErr != nil || !ok || seen[key] {
			return supply.CancelResult{}, errors.New("invalid provider cancel response")
		}
		seen[key] = true
		var value string
		if err = decoder.Decode(&value); err != nil {
			return supply.CancelResult{}, errors.New("invalid provider cancel response")
		}
		switch key {
		case "disposition":
			disposition = value
		case "observation_id":
			observationID = value
		case "observed_at":
			observedAt = value
		default:
			return supply.CancelResult{}, errors.New("invalid provider cancel response")
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return supply.CancelResult{}, errors.New("invalid provider cancel response")
	}
	if token, err = decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return supply.CancelResult{}, errors.New("invalid provider cancel response")
	}
	switch supply.CancelDisposition(disposition) {
	case supply.CancelUnknown:
		if observationID != "" || observedAt != "" {
			return supply.CancelResult{}, errors.New("invalid provider cancel response")
		}
		return supply.CancelResult{Disposition: supply.CancelUnknown}, nil
	case supply.CancelAcknowledged:
		parsedAt, ok := decodeObservedAt(observedAt)
		if !ok || !validObservationID(observationID) {
			return supply.CancelResult{}, errors.New("invalid provider cancel response")
		}
		return supply.CancelResult{Disposition: supply.CancelAcknowledged, ObservationID: observationID, ObservedAt: parsedAt}, nil
	default:
		return supply.CancelResult{}, errors.New("invalid provider cancel response")
	}
}

func (e *Executor) Cancel(ctx context.Context, query supply.CancelQuery) (supply.CancelResult, error) {
	if err := ctx.Err(); err != nil {
		return supply.CancelResult{}, err
	}
	if !validControlQuery(query.WorkspaceID, query.RunID, query.AttemptNo, query.ProviderID, query.ProviderRequestID, query.ExternalTaskID) || !validCancelKey(query.CancelKey) {
		return supply.CancelResult{}, supply.ErrProviderCancelUnavailable
	}
	prepared, err := e.prepareControl(ctx, query.WorkspaceID, query.RunID, query.ProviderID)
	if err != nil || !prepared.Deployment.SupportsCancellation() {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return supply.CancelResult{}, err
		}
		return supply.CancelResult{}, supply.ErrProviderCancelUnavailable
	}
	body, err := json.Marshal(struct {
		ProviderRequestID string `json:"provider_request_id"`
		ExternalTaskID    string `json:"external_task_id,omitempty"`
		CancelKey         string `json:"cancel_key"`
	}{query.ProviderRequestID, query.ExternalTaskID, query.CancelKey})
	if err != nil {
		return supply.CancelResult{}, supply.ErrProviderCancelUnavailable
	}
	response, uncertain, err := e.doControlJSON(ctx, prepared, prepared.Deployment.CancelEndpointURL, body, query.CancelKey)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return supply.CancelResult{}, err
	}
	if err != nil {
		return supply.CancelResult{}, supply.ErrProviderCancelUnavailable
	}
	if uncertain {
		return supply.CancelResult{Disposition: supply.CancelUnknown}, nil
	}
	result, err := decodeCancel(response)
	if err != nil {
		// The request may have been accepted even when the response is invalid.
		// Treat it as unknown so the durable sending intent is never reissued.
		return supply.CancelResult{Disposition: supply.CancelUnknown}, nil
	}
	return result, nil
}

var _ supply.ProviderStatusReader = (*Executor)(nil)
var _ supply.ProviderCanceler = (*Executor)(nil)
