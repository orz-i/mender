package execution_test

import (
	"context"
	"errors"
	"testing"
	"time"

	supplystatus "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/supplystatus"
	app "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type reconciliationTargetsFake struct {
	target app.ProviderTarget
	found  bool
	err    error
}

func TestProviderReconcilerBoundsProviderTimeByDurableSubmissionEvidence(t *testing.T) {
	evidenceAt := time.Date(2026, 9, 10, 22, 0, 2, 0, time.UTC)
	providerAt := evidenceAt.Add(-2 * time.Second)
	target := reconciliationTarget()
	target.EvidenceAt = evidenceAt

	for _, tc := range []struct {
		name       string
		providerAt time.Time
		want       time.Time
	}{
		{"provider clock before durable submission", providerAt, evidenceAt},
		{"provider clock after durable submission", evidenceAt.Add(time.Second), evidenceAt.Add(time.Second)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := &providerStatusSourceFake{status: app.ProviderStatus{
				ObservationID: "obs.time.bound", State: domain.ProviderSucceeded,
				ResultJSON: `{"ok":true}`, ObservedAt: tc.providerAt,
			}}
			sink := &providerResultSinkFake{}
			reconciler, err := app.NewProviderReconciler(&reconciliationTargetsFake{target: target, found: true}, status, sink)
			if err != nil {
				t.Fatal(err)
			}
			_, err = reconciler.ReconcileOne(context.Background(), target.WorkspaceID)
			if err != nil || sink.calls != 1 || !sink.last.ObservedAt.Equal(tc.want) {
				t.Fatal(err, sink.calls, sink.last.ObservedAt, tc.want)
			}
			if status.last.EvidenceAt != evidenceAt {
				t.Fatal("status source did not receive durable evidence bound", status.last)
			}
		})
	}
}

func (f *reconciliationTargetsFake) NextProviderTarget(context.Context, domain.WorkspaceID) (app.ProviderTarget, bool, error) {
	return f.target, f.found, f.err
}

type providerStatusSourceFake struct {
	status app.ProviderStatus
	err    error
	calls  int
	last   app.ProviderTarget
}

func (f *providerStatusSourceFake) QueryProviderStatus(_ context.Context, target app.ProviderTarget) (app.ProviderStatus, error) {
	f.calls++
	f.last = target
	return f.status, f.err
}

type providerResultSinkFake struct {
	calls int
	last  domain.ProviderObservation
	err   error
}

func (f *providerResultSinkFake) Observe(_ context.Context, observation domain.ProviderObservation) (app.ProviderResultRecord, error) {
	f.calls++
	f.last = observation
	if f.err != nil {
		return app.ProviderResultRecord{}, f.err
	}
	return app.ProviderResultRecord{Observation: observation}, nil
}

func reconciliationTarget() app.ProviderTarget {
	return app.ProviderTarget{WorkspaceID: "ws_reconcile", RunID: "run_reconcile", AttemptNo: 1, ProviderID: "provider_a", ProviderRequestID: "request/123", ExternalTaskID: "task:abc"}
}

func TestProviderReconcilerPersistsOnlyValidatedProviderObservation(t *testing.T) {
	at := time.Date(2026, 9, 10, 22, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		status app.ProviderStatus
	}{
		{"pending", app.ProviderStatus{ObservationID: "obs.pending.1", State: domain.ProviderPending, ObservedAt: at}},
		{"succeeded", app.ProviderStatus{ObservationID: "obs.success.1", State: domain.ProviderSucceeded, ResultJSON: `{"ok":true}`, ObservedAt: at}},
		{"failed", app.ProviderStatus{ObservationID: "obs.failed.1", State: domain.ProviderFailed, ErrorCode: "provider_error", ObservedAt: at}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			targets := &reconciliationTargetsFake{target: reconciliationTarget(), found: true}
			status := &providerStatusSourceFake{status: tc.status}
			sink := &providerResultSinkFake{}
			reconciler, err := app.NewProviderReconciler(targets, status, sink)
			if err != nil {
				t.Fatal(err)
			}
			record, err := reconciler.ReconcileOne(context.Background(), "ws_reconcile")
			if err != nil || status.calls != 1 || sink.calls != 1 || record.Observation.ObservationID != tc.status.ObservationID {
				t.Fatal(record, err, status.calls, sink.calls)
			}
			if sink.last.WorkspaceID != "ws_reconcile" || sink.last.RunID != "run_reconcile" || sink.last.AttemptNo != 1 || sink.last.ProviderRequestID != "request/123" || sink.last.ExternalTaskID != "task:abc" || sink.last.State != tc.status.State {
				t.Fatal("provider target was not preserved", sink.last)
			}
		})
	}
}

func TestProviderReconcilerFailsClosedBeforeResultWrite(t *testing.T) {
	at := time.Date(2026, 9, 10, 22, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name       string
		targets    *reconciliationTargetsFake
		status     *providerStatusSourceFake
		wantErr    error
		statusCall int
	}{
		{"no-target", &reconciliationTargetsFake{}, &providerStatusSourceFake{}, app.ErrNoProviderReconciliation, 0},
		{"invalid-target", &reconciliationTargetsFake{target: app.ProviderTarget{WorkspaceID: "ws_reconcile", RunID: "run_reconcile"}, found: true}, &providerStatusSourceFake{}, app.ErrInvalidProviderStatus, 0},
		{"provider-unavailable", &reconciliationTargetsFake{target: reconciliationTarget(), found: true}, &providerStatusSourceFake{err: errors.New("raw provider detail")}, app.ErrProviderStatusUnavailable, 1},
		{"invalid-status", &reconciliationTargetsFake{target: reconciliationTarget(), found: true}, &providerStatusSourceFake{status: app.ProviderStatus{ObservationID: "obs.invalid", State: domain.ProviderSucceeded, ResultJSON: `{`, ObservedAt: at}}, app.ErrInvalidProviderStatus, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := &providerResultSinkFake{}
			reconciler, err := app.NewProviderReconciler(tc.targets, tc.status, sink)
			if err != nil {
				t.Fatal(err)
			}
			_, err = reconciler.ReconcileOne(context.Background(), "ws_reconcile")
			if !errors.Is(err, tc.wantErr) || tc.status.calls != tc.statusCall || sink.calls != 0 {
				t.Fatal(err, tc.status.calls, sink.calls)
			}
		})
	}
}

type statusReaderFunc func(context.Context, supply.StatusQuery) (supply.StatusObservation, error)

func (f statusReaderFunc) QueryStatus(ctx context.Context, query supply.StatusQuery) (supply.StatusObservation, error) {
	return f(ctx, query)
}

func TestSupplyStatusSourceRoutesOnlyByDurableProviderIdentity(t *testing.T) {
	at := time.Date(2026, 9, 10, 22, 0, 0, 0, time.UTC)
	called := false
	source, err := supplystatus.New(map[string]supply.ProviderStatusReader{
		"provider_a": statusReaderFunc(func(_ context.Context, query supply.StatusQuery) (supply.StatusObservation, error) {
			called = true
			if query.WorkspaceID != "ws_reconcile" || query.RunID != "run_reconcile" || query.AttemptNo != 1 || query.ProviderID != "provider_a" || query.ProviderRequestID != "request/123" || query.ExternalTaskID != "task:abc" {
				t.Fatal(query)
			}
			return supply.StatusObservation{ObservationID: "obs.route.1", State: supply.StatusSucceeded, ResultJSON: `{"ok":true}`, ObservedAt: at}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := source.QueryProviderStatus(context.Background(), reconciliationTarget())
	if err != nil || !called || status.State != domain.ProviderSucceeded || status.ObservationID != "obs.route.1" {
		t.Fatal(status, err, called)
	}
	missing := reconciliationTarget()
	missing.ProviderID = "provider_b"
	if _, err = source.QueryProviderStatus(context.Background(), missing); !errors.Is(err, app.ErrProviderStatusUnavailable) {
		t.Fatal("unknown provider route was guessed", err)
	}
	if source, err = supplystatus.New(map[string]supply.ProviderStatusReader{"bad provider": statusReaderFunc(func(context.Context, supply.StatusQuery) (supply.StatusObservation, error) {
		return supply.StatusObservation{}, nil
	})}); !errors.Is(err, app.ErrProviderStatusUnavailable) || source != nil {
		t.Fatal(source, err)
	}
}
