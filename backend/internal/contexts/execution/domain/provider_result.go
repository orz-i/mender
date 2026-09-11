package domain

import (
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"
)

var ErrInvalidProviderObservation = errors.New("invalid provider observation")

type ProviderResultState string

const (
	ProviderPending   ProviderResultState = "pending"
	ProviderSucceeded ProviderResultState = "succeeded"
	ProviderFailed    ProviderResultState = "failed"
	ProviderCanceled  ProviderResultState = "canceled"
)

type ProviderObservation struct {
	WorkspaceID       WorkspaceID
	RunID             RunID
	ObservationID     string
	AttemptNo         uint32
	ProviderRequestID string
	ExternalTaskID    string
	State             ProviderResultState
	ResultJSON        string
	ErrorCode         string
	ObservedAt        time.Time
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

func validProviderErrorCode(value string) bool {
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

func validResultJSON(value string) bool {
	return len(value) >= 1 && len(value) <= 1<<20 && utf8.ValidString(value) && json.Valid([]byte(value))
}

func (o ProviderObservation) IsTerminal() bool {
	return o.State == ProviderSucceeded || o.State == ProviderFailed || o.State == ProviderCanceled
}

func (o ProviderObservation) Validate() error {
	if !o.WorkspaceID.IsValid() || !o.RunID.IsValid() || !validObservationID(o.ObservationID) || o.AttemptNo == 0 || o.AttemptNo > 100 || !validProviderValue(o.ProviderRequestID, true) || !validProviderValue(o.ExternalTaskID, false) || !validJobTime(o.ObservedAt) {
		return ErrInvalidProviderObservation
	}
	switch o.State {
	case ProviderPending:
		if o.ResultJSON != "" || o.ErrorCode != "" {
			return ErrInvalidProviderObservation
		}
	case ProviderSucceeded:
		if !validResultJSON(o.ResultJSON) || o.ErrorCode != "" {
			return ErrInvalidProviderObservation
		}
	case ProviderFailed:
		if o.ResultJSON != "" || !validProviderErrorCode(o.ErrorCode) {
			return ErrInvalidProviderObservation
		}
	case ProviderCanceled:
		if o.ResultJSON != "" || o.ErrorCode != "" {
			return ErrInvalidProviderObservation
		}
	default:
		return ErrInvalidProviderObservation
	}
	return nil
}
