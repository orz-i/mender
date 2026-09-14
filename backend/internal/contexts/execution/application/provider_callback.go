package application

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"
)

var (
	ErrProviderCallbackInvalid     = errors.New("invalid provider callback")
	ErrProviderCallbackConflict    = errors.New("provider callback conflict")
	ErrProviderCallbackUnavailable = errors.New("provider callback unavailable")
)

type ProviderCallback struct {
	ProviderID, EventID, BodySHA256, KeyID, WorkspaceID, RunID string
	AttemptNo                                                  uint32
	ProviderRequestID, ExternalTaskID, ObservationID           string
	State, ResultJSON, ErrorCode                               string
	SignedAt, ReceivedAt, ObservedAt                           time.Time
}

type ProviderCallbackDisposition string

const (
	ProviderCallbackAccepted    ProviderCallbackDisposition = "accepted"
	ProviderCallbackQuarantined ProviderCallbackDisposition = "quarantined"
	ProviderCallbackDuplicate   ProviderCallbackDisposition = "duplicate"
)

type ProviderCallbackReceipt struct {
	ReceiptID, ProviderID, EventID, WorkspaceID, RunID, ObservationID string
	Disposition                                                       ProviderCallbackDisposition
	ReasonCode                                                        string
	ReceivedAt, ProcessedAt                                           time.Time
}

type ProviderCallbackRepository interface {
	IngestProviderCallback(context.Context, ProviderCallback) (ProviderCallbackReceipt, error)
}

type ProviderCallbackService struct{ repository ProviderCallbackRepository }

func NewProviderCallbackService(repository ProviderCallbackRepository) (*ProviderCallbackService, error) {
	if repository == nil {
		return nil, ErrProviderCallbackUnavailable
	}
	return &ProviderCallbackService{repository: repository}, nil
}

func validCallbackID(value string, max int, extended bool) bool {
	if len(value) < 1 || len(value) > max {
		return false
	}
	for _, ch := range value {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' {
			continue
		}
		if extended && (ch == '.' || ch == ':') {
			continue
		}
		return false
	}
	return true
}

func validCallbackHandle(value string, required bool) bool {
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

func validProviderCallback(callback ProviderCallback) bool {
	if !validCallbackID(callback.ProviderID, 128, false) || !validCallbackID(callback.EventID, 200, true) ||
		!validCallbackID(callback.KeyID, 128, false) || !validCallbackID(callback.WorkspaceID, 128, false) ||
		!validCallbackID(callback.RunID, 128, false) || !validCallbackID(callback.ObservationID, 200, true) ||
		len(callback.BodySHA256) != 64 || callback.AttemptNo < 1 || callback.AttemptNo > 100 ||
		!validCallbackHandle(callback.ProviderRequestID, true) || !validCallbackHandle(callback.ExternalTaskID, false) ||
		callback.SignedAt.IsZero() || callback.ReceivedAt.IsZero() || callback.ObservedAt.IsZero() {
		return false
	}
	for _, ch := range callback.BodySHA256 {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func (s *ProviderCallbackService) Ingest(ctx context.Context, callback ProviderCallback) (ProviderCallbackReceipt, error) {
	if err := ctx.Err(); err != nil {
		return ProviderCallbackReceipt{}, err
	}
	if !validProviderCallback(callback) {
		return ProviderCallbackReceipt{}, ErrProviderCallbackInvalid
	}
	return s.repository.IngestProviderCallback(ctx, callback)
}
