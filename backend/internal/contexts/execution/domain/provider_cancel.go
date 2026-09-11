package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalidProviderCancel = errors.New("invalid provider cancellation")
	ErrProviderCancelState   = errors.New("invalid provider cancellation state")
)

type ProviderCancelState string

const (
	ProviderCancelRequested  ProviderCancelState = "requested"
	ProviderCancelSending    ProviderCancelState = "sending"
	ProviderCancelUnknown    ProviderCancelState = "unknown"
	ProviderCancelFulfilled  ProviderCancelState = "fulfilled"
	ProviderCancelSuperseded ProviderCancelState = "superseded"
)

type ProviderCancelSnapshot struct {
	WorkspaceID           WorkspaceID
	RunID                 RunID
	AttemptNo             uint32
	CancelKey             string
	ProviderID            string
	ProviderRequestID     string
	ExternalTaskID        string
	RequestedBySubject    string
	RequestedByCredential string
	Reason                string
	State                 ProviderCancelState
	RequestedAt           time.Time
	SendingAt             time.Time
	ResolvedAt            time.Time
	OutcomeObservationID  string
	UnknownReason         string
}

type ProviderCancelIntent struct{ snapshot ProviderCancelSnapshot }

func validCancelActor(value string) bool {
	return len(value) >= 1 && len(value) <= 512 && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func validCancelReason(value string) bool {
	return len([]rune(value)) <= 500 && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func ValidateProviderCancel(s ProviderCancelSnapshot) error {
	if !s.WorkspaceID.IsValid() || !s.RunID.IsValid() || s.AttemptNo == 0 || s.AttemptNo > 100 || !validSubmissionKey(s.CancelKey) || !validID(s.ProviderID) || !validProviderValue(s.ProviderRequestID, true) || !validProviderValue(s.ExternalTaskID, false) || !validCancelActor(s.RequestedBySubject) || !validCancelActor(s.RequestedByCredential) || !validCancelReason(s.Reason) || !validJobTime(s.RequestedAt) {
		return ErrInvalidProviderCancel
	}
	switch s.State {
	case ProviderCancelRequested:
		if !s.SendingAt.IsZero() || !s.ResolvedAt.IsZero() || s.OutcomeObservationID != "" || s.UnknownReason != "" {
			return ErrInvalidProviderCancel
		}
	case ProviderCancelSending:
		if !validJobTime(s.SendingAt) || s.SendingAt.Before(s.RequestedAt) || !s.ResolvedAt.IsZero() || s.OutcomeObservationID != "" || s.UnknownReason != "" {
			return ErrInvalidProviderCancel
		}
	case ProviderCancelUnknown:
		if !validJobTime(s.SendingAt) || s.SendingAt.Before(s.RequestedAt) || !validJobTime(s.ResolvedAt) || s.ResolvedAt.Before(s.SendingAt) || s.OutcomeObservationID != "" || !validUnknownReason(s.UnknownReason) {
			return ErrInvalidProviderCancel
		}
	case ProviderCancelFulfilled, ProviderCancelSuperseded:
		if !validJobTime(s.ResolvedAt) || s.ResolvedAt.Before(s.RequestedAt) || !validObservationID(s.OutcomeObservationID) || s.UnknownReason != "" || (!s.SendingAt.IsZero() && (!validJobTime(s.SendingAt) || s.SendingAt.Before(s.RequestedAt))) {
			return ErrInvalidProviderCancel
		}
	default:
		return ErrInvalidProviderCancel
	}
	return nil
}

func NewProviderCancelIntent(s ProviderCancelSnapshot) (ProviderCancelIntent, error) {
	s.State = ProviderCancelRequested
	s.SendingAt, s.ResolvedAt = time.Time{}, time.Time{}
	s.OutcomeObservationID, s.UnknownReason = "", ""
	if err := ValidateProviderCancel(s); err != nil {
		return ProviderCancelIntent{}, err
	}
	s.RequestedAt = s.RequestedAt.UTC()
	return ProviderCancelIntent{snapshot: s}, nil
}

func RestoreProviderCancelIntent(s ProviderCancelSnapshot) (ProviderCancelIntent, error) {
	if err := ValidateProviderCancel(s); err != nil {
		return ProviderCancelIntent{}, err
	}
	s.RequestedAt, s.SendingAt, s.ResolvedAt = s.RequestedAt.UTC(), s.SendingAt.UTC(), s.ResolvedAt.UTC()
	return ProviderCancelIntent{snapshot: s}, nil
}

func (i ProviderCancelIntent) Snapshot() ProviderCancelSnapshot { return i.snapshot }

func (i *ProviderCancelIntent) Claim(at time.Time) error {
	if i.snapshot.State != ProviderCancelRequested || !validJobTime(at) || at.Before(i.snapshot.RequestedAt) {
		return ErrProviderCancelState
	}
	i.snapshot.State, i.snapshot.SendingAt = ProviderCancelSending, at.UTC()
	return nil
}

func (i *ProviderCancelIntent) MarkUnknown(at time.Time, reason string) error {
	if i.snapshot.State != ProviderCancelSending || !validJobTime(at) || at.Before(i.snapshot.SendingAt) || !validUnknownReason(reason) {
		return ErrProviderCancelState
	}
	i.snapshot.State, i.snapshot.ResolvedAt, i.snapshot.UnknownReason = ProviderCancelUnknown, at.UTC(), reason
	return nil
}

func (i *ProviderCancelIntent) ResolveFromProvider(state ProviderResultState, at time.Time, observationID string) error {
	want := ProviderCancelSuperseded
	if state == ProviderCanceled {
		want = ProviderCancelFulfilled
	} else if state != ProviderSucceeded && state != ProviderFailed {
		return ErrProviderCancelState
	}
	if i.snapshot.State == ProviderCancelFulfilled || i.snapshot.State == ProviderCancelSuperseded {
		if i.snapshot.State == want && i.snapshot.ResolvedAt.Equal(at) && i.snapshot.OutcomeObservationID == observationID {
			return nil
		}
		return ErrProviderCancelState
	}
	if i.snapshot.State != ProviderCancelRequested && i.snapshot.State != ProviderCancelSending && i.snapshot.State != ProviderCancelUnknown || !validJobTime(at) || at.Before(i.snapshot.RequestedAt) || !validObservationID(observationID) {
		return ErrProviderCancelState
	}
	i.snapshot.State, i.snapshot.ResolvedAt, i.snapshot.OutcomeObservationID = want, at.UTC(), observationID
	i.snapshot.UnknownReason = ""
	return nil
}
