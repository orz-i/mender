package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/commerce/domain"
	"github.com/orz-i/mender/backend/internal/sharedkernel/canonicaljson"
)

const MaxPaymentCallbackBodyBytes = 64 << 10

var (
	ErrPaymentCallbackInvalid      = errors.New("payment callback invalid")
	ErrPaymentCallbackUnauthorized = errors.New("payment callback signature invalid")
	ErrPaymentCallbackUnavailable  = errors.New("payment callback unavailable")
)

type PaymentIntentRequest struct {
	WorkspaceID, BusinessKey, ProviderID, ProviderAccountID string
	Mode, Purpose, BillingJournalID, Currency               string
	AmountMicro                                             int64
}

type PaymentIntentReceipt struct {
	IntentID string
	Replay   bool
}

type PaymentIntentView struct {
	ID, BusinessKey, Purpose, BillingJournalID, Currency string
	AmountMicro                                          int64
	State                                                string
	Revision                                             int64
	CreatedAt, UpdatedAt, SettledAt, FailedAt            time.Time
}

type PaymentCallbackView struct {
	ReceiptID, EventID, IntentID, EventType, Currency string
	AmountMicro                                       int64
	EventState, Disposition, ReasonCode               string
	OccurredAt, ReceivedAt                            time.Time
	DeliveryCount                                     int
}

type PaymentAdminRepository interface {
	CreateSandboxPaymentIntent(context.Context, PaymentIntentRequest, string, string, time.Time) (PaymentIntentReceipt, error)
	ListPaymentIntents(context.Context, string, string, string, string, int) ([]PaymentIntentView, error)
	ListPaymentCallbacks(context.Context, string, string, string, string, int) ([]PaymentCallbackView, error)
	PaymentReconciliation(context.Context, string, string, string, string, string) (domain.PaymentReconciliationSummary, error)
}

type PaymentIDs interface{ NewPaymentIntentID() (string, error) }

type PaymentAdminService struct {
	repository PaymentAdminRepository
	auth       BillingAuthorizer
	ids        PaymentIDs
	clock      BillingClock
}

func NewPaymentAdminService(repository PaymentAdminRepository, auth BillingAuthorizer, ids PaymentIDs, clock BillingClock) (*PaymentAdminService, error) {
	if repository == nil || auth == nil || ids == nil || clock == nil {
		return nil, ErrBillingUnavailable
	}
	return &PaymentAdminService{repository: repository, auth: auth, ids: ids, clock: clock}, nil
}

func validPaymentHandle(value string, max int) bool {
	if len(value) < 1 || len(value) > max {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' || ch == '.' || ch == ':') {
			return false
		}
	}
	return true
}

func validPaymentIntentRequest(value PaymentIntentRequest) bool {
	return validID(value.WorkspaceID) && billingBusinessKey(value.BusinessKey) && validID(value.ProviderID) && validPaymentHandle(value.ProviderAccountID, 128) &&
		value.Mode == string(domain.PaymentModeSandbox) && (value.Purpose == string(domain.PaymentCollectCharge) || value.Purpose == string(domain.PaymentExecuteRefund)) &&
		validID(value.BillingJournalID) && validCurrency(value.Currency) && value.AmountMicro > 0
}

func (s *PaymentAdminService) CreateIntent(ctx context.Context, actor BillingActor, request PaymentIntentRequest) (PaymentIntentReceipt, error) {
	if !validID(actor.UserID) || !validPaymentIntentRequest(request) {
		return PaymentIntentReceipt{}, ErrBillingInvalid
	}
	if err := s.auth.AuthorizeBillingPlatform(ctx, actor, "platform:operate"); err != nil {
		return PaymentIntentReceipt{}, err
	}
	id, err := s.ids.NewPaymentIntentID()
	if err != nil || !validID(id) {
		return PaymentIntentReceipt{}, ErrBillingUnavailable
	}
	at, err := s.now()
	if err != nil {
		return PaymentIntentReceipt{}, err
	}
	receipt, err := s.repository.CreateSandboxPaymentIntent(ctx, request, id, actor.UserID, at)
	if err != nil {
		return PaymentIntentReceipt{}, err
	}
	if receipt.IntentID != id || !validID(receipt.IntentID) {
		return PaymentIntentReceipt{}, ErrBillingUnavailable
	}
	return receipt, nil
}

func validPaymentIntentView(value PaymentIntentView) bool {
	if !validID(value.ID) || !billingBusinessKey(value.BusinessKey) || !validID(value.BillingJournalID) || !validCurrency(value.Currency) || value.AmountMicro <= 0 || value.Revision < 1 || value.CreatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
		return false
	}
	if value.Purpose != string(domain.PaymentCollectCharge) && value.Purpose != string(domain.PaymentExecuteRefund) {
		return false
	}
	switch value.State {
	case string(domain.PaymentIntentPending):
		return value.Revision == 1 && value.SettledAt.IsZero() && value.FailedAt.IsZero()
	case string(domain.PaymentIntentSettled):
		return value.Revision == 2 && !value.SettledAt.IsZero() && value.FailedAt.IsZero()
	case string(domain.PaymentIntentFailed):
		return value.Revision == 2 && value.SettledAt.IsZero() && !value.FailedAt.IsZero()
	default:
		return false
	}
}

func validPaymentCallbackView(value PaymentCallbackView) bool {
	if len(value.ReceiptID) != 64 || !validPaymentHandle(value.EventID, 200) || !validID(value.IntentID) || !validPaymentHandle(value.EventType, 128) || !validCurrency(value.Currency) || value.AmountMicro <= 0 || value.OccurredAt.IsZero() || value.ReceivedAt.IsZero() || value.DeliveryCount < 1 {
		return false
	}
	for _, ch := range value.ReceiptID {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	if value.EventState != string(domain.PaymentEventSucceeded) && value.EventState != string(domain.PaymentEventFailed) {
		return false
	}
	switch value.Disposition {
	case string(domain.PaymentEventAccepted):
		return value.ReasonCode == ""
	case string(domain.PaymentEventQuarantined):
		return validPaymentHandle(value.ReasonCode, 128)
	default:
		return false
	}
}

func (s *PaymentAdminService) Intents(ctx context.Context, actor BillingActor, workspace, provider, account string) ([]PaymentIntentView, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(provider) || !validPaymentHandle(account, 128) {
		return nil, ErrBillingInvalid
	}
	if err := s.auth.AuthorizeBillingPlatform(ctx, actor, "platform:audit"); err != nil {
		return nil, err
	}
	items, err := s.repository.ListPaymentIntents(ctx, workspace, actor.UserID, provider, account, 100)
	if err != nil {
		return nil, err
	}
	if len(items) > 100 {
		return nil, ErrBillingUnavailable
	}
	for _, item := range items {
		if !validPaymentIntentView(item) {
			return nil, ErrBillingUnavailable
		}
	}
	return items, nil
}

func (s *PaymentAdminService) Callbacks(ctx context.Context, actor BillingActor, workspace, provider, account string) ([]PaymentCallbackView, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(provider) || !validPaymentHandle(account, 128) {
		return nil, ErrBillingInvalid
	}
	if err := s.auth.AuthorizeBillingPlatform(ctx, actor, "platform:audit"); err != nil {
		return nil, err
	}
	items, err := s.repository.ListPaymentCallbacks(ctx, workspace, actor.UserID, provider, account, 100)
	if err != nil {
		return nil, err
	}
	if len(items) > 100 {
		return nil, ErrBillingUnavailable
	}
	for _, item := range items {
		if !validPaymentCallbackView(item) {
			return nil, ErrBillingUnavailable
		}
	}
	return items, nil
}

func (s *PaymentAdminService) Reconciliation(ctx context.Context, actor BillingActor, workspace, provider, account, currency string) (domain.PaymentReconciliationSummary, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(provider) || !validPaymentHandle(account, 128) || !validCurrency(currency) {
		return domain.PaymentReconciliationSummary{}, ErrBillingInvalid
	}
	if err := s.auth.AuthorizeBillingPlatform(ctx, actor, "platform:audit"); err != nil {
		return domain.PaymentReconciliationSummary{}, err
	}
	value, err := s.repository.PaymentReconciliation(ctx, workspace, actor.UserID, provider, account, currency)
	if err != nil {
		return domain.PaymentReconciliationSummary{}, err
	}
	if value.WorkspaceID != workspace || value.ProviderID != provider || value.ProviderAccountID != account || value.Currency != currency || !value.Valid() {
		return domain.PaymentReconciliationSummary{}, ErrBillingUnavailable
	}
	return value, nil
}

func (s *PaymentAdminService) now() (time.Time, error) {
	at := s.clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() || at.Year() > 9999 {
		return time.Time{}, ErrBillingUnavailable
	}
	return at, nil
}

type PaymentCallbackSecret struct{ value []byte }

func NewPaymentCallbackSecret(value []byte) (PaymentCallbackSecret, error) {
	if len(value) < 32 || len(value) > 256 {
		return PaymentCallbackSecret{}, ErrPaymentCallbackUnavailable
	}
	return PaymentCallbackSecret{value: append([]byte(nil), value...)}, nil
}

func (s PaymentCallbackSecret) Bytes() []byte    { return append([]byte(nil), s.value...) }
func (s PaymentCallbackSecret) String() string   { return "[REDACTED]" }
func (s PaymentCallbackSecret) GoString() string { return "[REDACTED]" }

type PaymentCallbackSecretSource interface {
	ResolvePaymentCallbackSecret(context.Context, string, string, string) (PaymentCallbackSecret, error)
}

type PaymentCallbackVerification struct {
	SignedAt   time.Time
	BodySHA256 string
}

type PaymentCallbackVerifier interface {
	VerifyPaymentCallback(context.Context, string, string, string, string, string, []byte, time.Time) (PaymentCallbackVerification, error)
}

type VerifiedPaymentCallback struct {
	ProviderID, ProviderAccountID, EventID, BodySHA256, KeyID string
	WorkspaceID, IntentID, EventType, ProviderTransactionID   string
	Currency, EventState                                      string
	AmountMicro                                               int64
	SignedAt, ReceivedAt, OccurredAt                          time.Time
}

type PaymentCallbackReceipt struct {
	ReceiptID, EventID, Disposition, ReasonCode string
}

type PaymentCallbackReceiver interface {
	IngestPaymentCallback(context.Context, VerifiedPaymentCallback) (PaymentCallbackReceipt, error)
}

type PaymentCallbackService struct {
	receiver PaymentCallbackReceiver
	verifier PaymentCallbackVerifier
	clock    BillingClock
}

func NewPaymentCallbackService(receiver PaymentCallbackReceiver, verifier PaymentCallbackVerifier, clock BillingClock) (*PaymentCallbackService, error) {
	if receiver == nil || verifier == nil || clock == nil {
		return nil, ErrPaymentCallbackUnavailable
	}
	return &PaymentCallbackService{receiver: receiver, verifier: verifier, clock: clock}, nil
}

func paymentString(raw json.RawMessage, nullable bool) (string, bool) {
	if nullable && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", true
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func paymentExactKeys(raw map[string]json.RawMessage, keys ...string) bool {
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

func decodePaymentCallback(provider, account, keyID, bodyHash string, signedAt, receivedAt time.Time, canonical []byte) (VerifiedPaymentCallback, error) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(canonical, &raw) != nil || !paymentExactKeys(raw, "schema_version", "event_type", "event_id", "workspace_id", "intent_id", "provider_transaction_id", "currency", "amount_micro", "occurred_at") {
		return VerifiedPaymentCallback{}, ErrPaymentCallbackInvalid
	}
	var version int
	if json.Unmarshal(raw["schema_version"], &version) != nil || version != 1 {
		return VerifiedPaymentCallback{}, ErrPaymentCallbackInvalid
	}
	eventType, ok := paymentString(raw["event_type"], false)
	if !ok || (eventType != "payment.succeeded" && eventType != "payment.failed" && eventType != "refund.succeeded" && eventType != "refund.failed") {
		return VerifiedPaymentCallback{}, ErrPaymentCallbackInvalid
	}
	eventID, ok := paymentString(raw["event_id"], false)
	if !ok || !validPaymentHandle(eventID, 200) {
		return VerifiedPaymentCallback{}, ErrPaymentCallbackInvalid
	}
	workspace, ok := paymentString(raw["workspace_id"], false)
	if !ok || !validID(workspace) {
		return VerifiedPaymentCallback{}, ErrPaymentCallbackInvalid
	}
	intent, ok := paymentString(raw["intent_id"], false)
	if !ok || !validID(intent) {
		return VerifiedPaymentCallback{}, ErrPaymentCallbackInvalid
	}
	providerTransaction, ok := paymentString(raw["provider_transaction_id"], true)
	if !ok || providerTransaction != "" && !validPaymentHandle(providerTransaction, 200) {
		return VerifiedPaymentCallback{}, ErrPaymentCallbackInvalid
	}
	currency, ok := paymentString(raw["currency"], false)
	if !ok || !validCurrency(currency) {
		return VerifiedPaymentCallback{}, ErrPaymentCallbackInvalid
	}
	amountRaw, ok := paymentString(raw["amount_micro"], false)
	amount, err := strconv.ParseInt(amountRaw, 10, 64)
	if !ok || err != nil || amount <= 0 || strconv.FormatInt(amount, 10) != amountRaw {
		return VerifiedPaymentCallback{}, ErrPaymentCallbackInvalid
	}
	occurredRaw, ok := paymentString(raw["occurred_at"], false)
	if !ok {
		return VerifiedPaymentCallback{}, ErrPaymentCallbackInvalid
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, occurredRaw)
	if err != nil {
		return VerifiedPaymentCallback{}, ErrPaymentCallbackInvalid
	}
	occurredAt = occurredAt.UTC().Truncate(time.Microsecond)
	if occurredAt.IsZero() || occurredAt.After(receivedAt.Add(time.Minute)) {
		return VerifiedPaymentCallback{}, ErrPaymentCallbackInvalid
	}
	state := string(domain.PaymentEventFailed)
	if eventType == "payment.succeeded" || eventType == "refund.succeeded" {
		state = string(domain.PaymentEventSucceeded)
		if providerTransaction == "" {
			return VerifiedPaymentCallback{}, ErrPaymentCallbackInvalid
		}
	} else if providerTransaction != "" {
		return VerifiedPaymentCallback{}, ErrPaymentCallbackInvalid
	}
	return VerifiedPaymentCallback{
		ProviderID: provider, ProviderAccountID: account, EventID: eventID, BodySHA256: bodyHash, KeyID: keyID,
		WorkspaceID: workspace, IntentID: intent, EventType: eventType, ProviderTransactionID: providerTransaction,
		Currency: currency, EventState: state, AmountMicro: amount, SignedAt: signedAt, ReceivedAt: receivedAt, OccurredAt: occurredAt,
	}, nil
}

func (s *PaymentCallbackService) Handle(ctx context.Context, provider, account, keyID, timestamp, signature string, raw []byte) (PaymentCallbackReceipt, error) {
	if err := ctx.Err(); err != nil {
		return PaymentCallbackReceipt{}, err
	}
	if !validID(provider) || !validPaymentHandle(account, 128) || !validID(keyID) || len(raw) < 2 || len(raw) > MaxPaymentCallbackBodyBytes {
		return PaymentCallbackReceipt{}, ErrPaymentCallbackInvalid
	}
	receivedAt := s.clock.Now().UTC().Truncate(time.Microsecond)
	if receivedAt.IsZero() {
		return PaymentCallbackReceipt{}, ErrPaymentCallbackUnavailable
	}
	verification, err := s.verifier.VerifyPaymentCallback(ctx, provider, account, keyID, timestamp, signature, raw, receivedAt)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return PaymentCallbackReceipt{}, err
		}
		if errors.Is(err, ErrPaymentCallbackUnauthorized) {
			return PaymentCallbackReceipt{}, ErrPaymentCallbackUnauthorized
		}
		return PaymentCallbackReceipt{}, ErrPaymentCallbackUnavailable
	}
	if len(verification.BodySHA256) != 64 || verification.SignedAt.IsZero() {
		return PaymentCallbackReceipt{}, ErrPaymentCallbackUnavailable
	}
	canonical, err := canonicaljson.Object(raw, MaxPaymentCallbackBodyBytes)
	if err != nil {
		return PaymentCallbackReceipt{}, ErrPaymentCallbackInvalid
	}
	callback, err := decodePaymentCallback(provider, account, keyID, verification.BodySHA256, verification.SignedAt, receivedAt, canonical)
	if err != nil {
		return PaymentCallbackReceipt{}, err
	}
	receipt, err := s.receiver.IngestPaymentCallback(ctx, callback)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return PaymentCallbackReceipt{}, err
		}
		return PaymentCallbackReceipt{}, ErrPaymentCallbackUnavailable
	}
	if receipt.EventID != callback.EventID || !validPaymentHandle(receipt.ReceiptID, 64) ||
		(receipt.Disposition != string(domain.PaymentEventAccepted) && receipt.Disposition != string(domain.PaymentEventDuplicate) && receipt.Disposition != string(domain.PaymentEventQuarantined)) ||
		(receipt.Disposition == string(domain.PaymentEventQuarantined)) != (receipt.ReasonCode != "") {
		return PaymentCallbackReceipt{}, ErrPaymentCallbackUnavailable
	}
	return receipt, nil
}
