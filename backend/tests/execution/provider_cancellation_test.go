package execution_test

import (
	"context"
	"errors"
	"testing"
	"time"

	supplycancel "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/supplycancel"
	app "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

func providerCancelFixture(t *testing.T) (domain.ProviderCancelIntent, time.Time) {
	t.Helper()
	at := time.Date(2026, 9, 10, 23, 0, 0, 0, time.UTC)
	intent, err := domain.NewProviderCancelIntent(domain.ProviderCancelSnapshot{
		WorkspaceID: "ws_cancel", RunID: "run_cancel", AttemptNo: 1, CancelKey: "mender.cancel.run_cancel.1",
		ProviderID: "provider_a", ProviderRequestID: "request/1", ExternalTaskID: "task/1",
		RequestedBySubject: "sa_cancel", RequestedByCredential: "key_cancel", Reason: "user requested", RequestedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	return intent, at
}

func TestProviderCancelIntentIsSingleClaimAndTerminalEvidenceWins(t *testing.T) {
	intent, at := providerCancelFixture(t)
	if err := intent.Claim(at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := intent.Claim(at.Add(2 * time.Second)); !errors.Is(err, domain.ErrProviderCancelState) {
		t.Fatal("sending cancellation was claimable twice", err)
	}
	if err := intent.MarkUnknown(at.Add(2*time.Second), "provider cancellation outcome unknown"); err != nil {
		t.Fatal(err)
	}
	if err := intent.Claim(at.Add(3 * time.Second)); !errors.Is(err, domain.ErrProviderCancelState) {
		t.Fatal("unknown cancellation was reissued", err)
	}
	if err := intent.ResolveFromProvider(domain.ProviderCanceled, at.Add(4*time.Second), "obs.cancel.1"); err != nil {
		t.Fatal(err)
	}
	if snapshot := intent.Snapshot(); snapshot.State != domain.ProviderCancelFulfilled || snapshot.OutcomeObservationID != "obs.cancel.1" {
		t.Fatal(snapshot)
	}

	superseded, at := providerCancelFixture(t)
	if err := superseded.Claim(at.Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// Remote success may have happened after the user request but before the
	// cancellation call was actually sent; it still deterministically wins.
	if err := superseded.ResolveFromProvider(domain.ProviderSucceeded, at.Add(2*time.Second), "obs.success.1"); err != nil {
		t.Fatal(err)
	}
	if snapshot := superseded.Snapshot(); snapshot.State != domain.ProviderCancelSuperseded {
		t.Fatal(snapshot)
	}
}

type cancelClock struct{ at time.Time }

func (c *cancelClock) Now() time.Time { return c.at }

type cancelControlFake struct {
	target         app.ProviderCancelTarget
	found          bool
	claimCalls     int
	unknownCalls   int
	ackCalls       int
	unknownReason  string
	ackObservation string
	ackObservedAt  time.Time
}

func (f *cancelControlFake) ClaimProviderCancel(context.Context, domain.WorkspaceID, time.Time) (app.ProviderCancelTarget, bool, error) {
	f.claimCalls++
	return f.target, f.found, nil
}
func (f *cancelControlFake) RecordProviderCancelUnknown(_ context.Context, target app.ProviderCancelTarget, _ time.Time, reason string) error {
	f.unknownCalls++
	f.unknownReason = reason
	if target.CancelKey != f.target.CancelKey {
		return app.ErrInvalidProviderCancelResult
	}
	return nil
}
func (f *cancelControlFake) RecordProviderCancelAcknowledged(_ context.Context, target app.ProviderCancelTarget, observationID string, observedAt time.Time) (app.ProviderResultRecord, error) {
	f.ackCalls++
	f.ackObservation, f.ackObservedAt = observationID, observedAt
	if target.CancelKey != f.target.CancelKey {
		return app.ProviderResultRecord{}, app.ErrInvalidProviderCancelResult
	}
	return app.ProviderResultRecord{Observation: domain.ProviderObservation{WorkspaceID: target.WorkspaceID, RunID: target.RunID, ObservationID: observationID, AttemptNo: target.AttemptNo, ProviderID: target.ProviderID, ProviderRequestID: target.ProviderRequestID, ExternalTaskID: target.ExternalTaskID, State: domain.ProviderCanceled, ObservedAt: observedAt}}, nil
}

type cancelSourceFunc func(context.Context, app.ProviderCancelTarget) (app.ProviderCancelResult, error)

func (f cancelSourceFunc) CancelProvider(ctx context.Context, target app.ProviderCancelTarget) (app.ProviderCancelResult, error) {
	return f(ctx, target)
}

func cancelTargetFixture(at time.Time) app.ProviderCancelTarget {
	return app.ProviderCancelTarget{WorkspaceID: "ws_cancel", RunID: "run_cancel", AttemptNo: 1, CancelKey: "mender.cancel.run_cancel.1", ProviderID: "provider_a", ProviderRequestID: "request/1", ExternalTaskID: "task/1", RequestedAt: at, SendingAt: at.Add(time.Second)}
}

func TestProviderCancelDispatcherNeverBlindRetriesAfterClaim(t *testing.T) {
	at := time.Date(2026, 9, 10, 23, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name        string
		source      cancelSourceFunc
		wantErr     error
		wantAck     int
		wantUnknown int
	}{
		{"ack", func(context.Context, app.ProviderCancelTarget) (app.ProviderCancelResult, error) {
			return app.ProviderCancelResult{Disposition: app.ProviderCancelAcknowledged, ObservationID: "obs.cancel.ack", ObservedAt: at.Add(2 * time.Second)}, nil
		}, nil, 1, 0},
		{"unknown", func(context.Context, app.ProviderCancelTarget) (app.ProviderCancelResult, error) {
			return app.ProviderCancelResult{Disposition: app.ProviderCancelUnconfirmed}, nil
		}, app.ErrProviderCancelOutcomeUnknown, 0, 1},
		{"provider-error", func(context.Context, app.ProviderCancelTarget) (app.ProviderCancelResult, error) {
			return app.ProviderCancelResult{}, errors.New("raw-provider-secret-detail")
		}, app.ErrProviderCancelOutcomeUnknown, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			control := &cancelControlFake{target: cancelTargetFixture(at), found: true}
			clock := &cancelClock{at: at.Add(1500 * time.Millisecond)}
			dispatcher, err := app.NewProviderCancelDispatcher(control, tc.source, clock)
			if err != nil {
				t.Fatal(err)
			}
			_, err = dispatcher.CancelOne(context.Background(), "ws_cancel")
			if tc.wantErr == nil && err != nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) || control.claimCalls != 1 || control.ackCalls != tc.wantAck || control.unknownCalls != tc.wantUnknown {
				t.Fatal(err, control)
			}
			if control.unknownCalls > 0 && control.unknownReason != "provider cancellation outcome unknown" {
				t.Fatal("raw provider error leaked into durable cancellation reason", control.unknownReason)
			}
		})
	}

	control := &cancelControlFake{}
	called := false
	dispatcher, err := app.NewProviderCancelDispatcher(control, cancelSourceFunc(func(context.Context, app.ProviderCancelTarget) (app.ProviderCancelResult, error) {
		called = true
		return app.ProviderCancelResult{}, nil
	}), &cancelClock{at: at})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dispatcher.CancelOne(context.Background(), "ws_cancel"); !errors.Is(err, app.ErrNoProviderCancellation) || called {
		t.Fatal("no-target cancellation reached provider", err, called)
	}
}

type providerCancelerFunc func(context.Context, supply.CancelQuery) (supply.CancelResult, error)

func (f providerCancelerFunc) Cancel(ctx context.Context, query supply.CancelQuery) (supply.CancelResult, error) {
	return f(ctx, query)
}

func TestSupplyCancelSourceUsesExactProviderRoute(t *testing.T) {
	at := time.Date(2026, 9, 10, 23, 0, 0, 0, time.UTC)
	called := false
	source, err := supplycancel.New(map[string]supply.ProviderCanceler{
		"provider_a": providerCancelerFunc(func(_ context.Context, query supply.CancelQuery) (supply.CancelResult, error) {
			called = true
			if query.WorkspaceID != "ws_cancel" || query.RunID != "run_cancel" || query.AttemptNo != 1 || query.ProviderID != "provider_a" || query.ProviderRequestID != "request/1" || query.CancelKey != "mender.cancel.run_cancel.1" {
				t.Fatal(query)
			}
			return supply.CancelResult{Disposition: supply.CancelAcknowledged, ObservationID: "obs.cancel.route", ObservedAt: at.Add(2 * time.Second)}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.CancelProvider(context.Background(), cancelTargetFixture(at))
	if err != nil || !called || result.Disposition != app.ProviderCancelAcknowledged || result.ObservationID != "obs.cancel.route" {
		t.Fatal(result, err, called)
	}
	missing := cancelTargetFixture(at)
	missing.ProviderID = "provider_b"
	if _, err = source.CancelProvider(context.Background(), missing); !errors.Is(err, app.ErrProviderCancelUnavailable) {
		t.Fatal("unknown provider cancel route was guessed", err)
	}
}
