package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/orz-i/mender/backend/internal/sharedkernel/canonicaljson"
)

const MaxBodyBytes = 1 << 20

var (
	ErrInvalid      = errors.New("provider callback invalid")
	ErrUnauthorized = errors.New("provider callback signature invalid")
	ErrUnavailable  = errors.New("provider callback unavailable")
)

type Secret struct{ value []byte }

func NewSecret(value []byte) (Secret, error) {
	if len(value) < 32 || len(value) > 256 {
		return Secret{}, ErrUnavailable
	}
	return Secret{value: append([]byte(nil), value...)}, nil
}

func (s Secret) Bytes() []byte    { return append([]byte(nil), s.value...) }
func (s Secret) String() string   { return "[REDACTED]" }
func (s Secret) GoString() string { return "[REDACTED]" }

type SecretSource interface {
	ResolveCallbackSecret(context.Context, string, string) (Secret, error)
}

type Verification struct {
	SignedAt   time.Time
	BodySHA256 string
}

type Verifier interface {
	Verify(context.Context, string, string, string, string, []byte, time.Time) (Verification, error)
}

type Callback struct {
	ProviderID, EventID, BodySHA256, KeyID, WorkspaceID, RunID string
	AttemptNo                                                  uint32
	ProviderRequestID, ExternalTaskID, ObservationID           string
	State, ResultJSON, ErrorCode                               string
	SignedAt, ReceivedAt, ObservedAt                           time.Time
}

type Disposition string

const (
	Accepted    Disposition = "accepted"
	Quarantined Disposition = "quarantined"
	Duplicate   Disposition = "duplicate"
)

type Receipt struct {
	ReceiptID, ProviderID, EventID, WorkspaceID, RunID, ObservationID string
	Disposition                                                       Disposition
	ReasonCode                                                        string
	ReceivedAt, ProcessedAt                                           time.Time
}

type Receiver interface {
	IngestProviderCallback(context.Context, Callback) (Receipt, error)
}

type Clock interface{ Now() time.Time }

type Service struct {
	receiver Receiver
	verifier Verifier
	clock    Clock
}

func New(receiver Receiver, verifier Verifier, clock Clock) (*Service, error) {
	if receiver == nil || verifier == nil || clock == nil {
		return nil, ErrUnavailable
	}
	return &Service{receiver: receiver, verifier: verifier, clock: clock}, nil
}

func validID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func validCallbackID(value string, max int) bool {
	if len(value) < 1 || len(value) > max {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == ':' || ch == '-') {
			return false
		}
	}
	return true
}

func validHandle(value string, required bool) bool {
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

func exactKeys(raw map[string]json.RawMessage, keys ...string) bool {
	if len(raw) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := raw[key]; !ok {
			return false
		}
	}
	return true
}

func decodeString(raw json.RawMessage, nullable bool) (string, bool) {
	if nullable && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", true
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func decodePayload(provider, keyID, bodyHash string, signedAt, receivedAt time.Time, canonical []byte) (Callback, error) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(canonical, &raw) != nil || !exactKeys(raw,
		"schema_version", "event_type", "event_id", "workspace_id", "run_id", "attempt_no", "provider_request_id", "external_task_id", "observation_id", "state", "result", "error_code", "occurred_at") {
		return Callback{}, ErrInvalid
	}
	var version int
	var attempt uint32
	if json.Unmarshal(raw["schema_version"], &version) != nil || version != 1 || json.Unmarshal(raw["attempt_no"], &attempt) != nil || attempt == 0 || attempt > 100 {
		return Callback{}, ErrInvalid
	}
	eventType, ok := decodeString(raw["event_type"], false)
	if !ok || eventType != "provider.observation" {
		return Callback{}, ErrInvalid
	}
	eventID, ok := decodeString(raw["event_id"], false)
	if !ok || !validCallbackID(eventID, 200) {
		return Callback{}, ErrInvalid
	}
	workspace, ok := decodeString(raw["workspace_id"], false)
	if !ok || !validID(workspace) {
		return Callback{}, ErrInvalid
	}
	runID, ok := decodeString(raw["run_id"], false)
	if !ok || !validID(runID) {
		return Callback{}, ErrInvalid
	}
	providerRequestID, ok := decodeString(raw["provider_request_id"], false)
	if !ok || !validHandle(providerRequestID, true) {
		return Callback{}, ErrInvalid
	}
	externalTaskID, ok := decodeString(raw["external_task_id"], true)
	if !ok || !validHandle(externalTaskID, false) {
		return Callback{}, ErrInvalid
	}
	observationID, ok := decodeString(raw["observation_id"], false)
	if !ok || !validCallbackID(observationID, 200) {
		return Callback{}, ErrInvalid
	}
	state, ok := decodeString(raw["state"], false)
	if !ok {
		return Callback{}, ErrInvalid
	}
	errorCode, ok := decodeString(raw["error_code"], true)
	if !ok || errorCode != "" && !validCallbackID(errorCode, 128) {
		return Callback{}, ErrInvalid
	}
	occurredRaw, ok := decodeString(raw["occurred_at"], false)
	if !ok {
		return Callback{}, ErrInvalid
	}
	observedAt, err := time.Parse(time.RFC3339Nano, occurredRaw)
	if err != nil {
		return Callback{}, ErrInvalid
	}
	observedAt = observedAt.UTC().Truncate(time.Microsecond)
	if observedAt.IsZero() || observedAt.After(receivedAt.Add(time.Minute)) {
		return Callback{}, ErrInvalid
	}
	resultJSON := ""
	resultNull := bytes.Equal(bytes.TrimSpace(raw["result"]), []byte("null"))
	if !resultNull {
		if !json.Valid(raw["result"]) || len(raw["result"]) > MaxBodyBytes {
			return Callback{}, ErrInvalid
		}
		resultJSON = string(raw["result"])
	}
	switch state {
	case "pending", "canceled":
		if !resultNull || errorCode != "" {
			return Callback{}, ErrInvalid
		}
	case "succeeded":
		if resultNull || errorCode != "" {
			return Callback{}, ErrInvalid
		}
	case "failed":
		if !resultNull || errorCode == "" {
			return Callback{}, ErrInvalid
		}
	default:
		return Callback{}, ErrInvalid
	}
	return Callback{
		ProviderID: provider, EventID: eventID, BodySHA256: bodyHash, KeyID: keyID,
		WorkspaceID: workspace, RunID: runID, AttemptNo: attempt, ProviderRequestID: providerRequestID,
		ExternalTaskID: externalTaskID, ObservationID: observationID, State: state, ResultJSON: resultJSON,
		ErrorCode: errorCode, SignedAt: signedAt, ReceivedAt: receivedAt, ObservedAt: observedAt,
	}, nil
}

func (s *Service) Handle(ctx context.Context, provider, keyID, timestamp, signature string, raw []byte) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if !validID(provider) || !validID(keyID) || len(raw) < 2 || len(raw) > MaxBodyBytes {
		return Receipt{}, ErrInvalid
	}
	receivedAt := s.clock.Now().UTC().Truncate(time.Microsecond)
	if receivedAt.IsZero() {
		return Receipt{}, ErrUnavailable
	}
	verification, err := s.verifier.Verify(ctx, provider, keyID, timestamp, signature, raw, receivedAt)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Receipt{}, err
		}
		if errors.Is(err, ErrUnauthorized) {
			return Receipt{}, ErrUnauthorized
		}
		return Receipt{}, ErrUnavailable
	}
	if len(verification.BodySHA256) != 64 || verification.SignedAt.IsZero() {
		return Receipt{}, ErrUnavailable
	}
	canonical, err := canonicaljson.Object(raw, MaxBodyBytes)
	if err != nil {
		return Receipt{}, ErrInvalid
	}
	callback, err := decodePayload(provider, keyID, verification.BodySHA256, verification.SignedAt, receivedAt, canonical)
	if err != nil {
		return Receipt{}, err
	}
	receipt, err := s.receiver.IngestProviderCallback(ctx, callback)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			return Receipt{}, ErrInvalid
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Receipt{}, err
		}
		return Receipt{}, ErrUnavailable
	}
	return receipt, nil
}
