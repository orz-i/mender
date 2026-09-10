package execution_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	app "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

type supervisorControlFake struct {
	mu              sync.Mutex
	lease           app.Lease
	activateCount   int
	leaseCount      int
	heartbeatCount  int
	releaseCount    int
	heartbeatErr    error
	leaseErr        error
	heartbeatSignal chan struct{}
}

func (f *supervisorControlFake) Activate(_ context.Context, workspace domain.WorkspaceID, revisions []string, limit int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if workspace != f.lease.WorkspaceID || len(revisions) == 0 || limit < 1 {
		return 0, app.ErrInvalidWorker
	}
	f.activateCount++
	return 1, nil
}

func (f *supervisorControlFake) LeaseOne(_ context.Context, workspace domain.WorkspaceID, worker string, _ time.Duration) (app.Lease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leaseCount++
	if f.leaseErr != nil {
		return app.Lease{}, f.leaseErr
	}
	if workspace != f.lease.WorkspaceID || worker != f.lease.WorkerID {
		return app.Lease{}, app.ErrInvalidWorker
	}
	return f.lease, nil
}

func (f *supervisorControlFake) Heartbeat(_ context.Context, lease app.Lease, ttl time.Duration) (app.Lease, error) {
	f.mu.Lock()
	f.heartbeatCount++
	err := f.heartbeatErr
	renewed := lease
	renewed.LeaseUntil = renewed.LeaseUntil.Add(ttl)
	signal := f.heartbeatSignal
	f.mu.Unlock()
	if signal != nil {
		select {
		case signal <- struct{}{}:
		default:
		}
	}
	if err != nil {
		return app.Lease{}, err
	}
	return renewed, nil
}

func (f *supervisorControlFake) ReleaseBeforeSubmit(_ context.Context, lease app.Lease, delay time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if lease.Generation != f.lease.Generation || delay != 0 {
		return app.ErrInvalidWorker
	}
	f.releaseCount++
	return nil
}

func (f *supervisorControlFake) counts() (activate, lease, heartbeat, release int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.activateCount, f.leaseCount, f.heartbeatCount, f.releaseCount
}

type supervisorDispatcherFunc func(context.Context, app.Lease) (app.DispatchResult, error)

func (f supervisorDispatcherFunc) Dispatch(ctx context.Context, lease app.Lease) (app.DispatchResult, error) {
	return f(ctx, lease)
}

type manualHeartbeatTicker struct {
	ch chan time.Time
}

func (t *manualHeartbeatTicker) C() <-chan time.Time { return t.ch }
func (t *manualHeartbeatTicker) Stop()               {}

type manualHeartbeatFactory struct {
	ticker *manualHeartbeatTicker
	err    error
}

func (f manualHeartbeatFactory) NewTicker(time.Duration) (app.HeartbeatTicker, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.ticker, nil
}

func supervisorLease() app.Lease {
	return app.Lease{
		WorkspaceID: "ws_supervisor",
		RunID:       "run_supervisor",
		WorkerID:    "worker_supervisor",
		Generation:  1,
		AttemptNo:   1,
		LeaseUntil:  time.Date(2026, 9, 10, 16, 0, 30, 0, time.UTC),
	}
}

func supervisorConfig() app.SupervisorConfig {
	return app.SupervisorConfig{
		WorkerID:          "worker_supervisor",
		LeaseTTL:          30 * time.Second,
		HeartbeatInterval: 5 * time.Second,
		ActivationLimit:   10,
	}
}

func TestDispatchSupervisorHeartbeatsWhileDispatcherRuns(t *testing.T) {
	ticker := &manualHeartbeatTicker{ch: make(chan time.Time, 1)}
	control := &supervisorControlFake{lease: supervisorLease(), heartbeatSignal: make(chan struct{}, 1)}
	dispatchStarted := make(chan struct{})
	finishDispatch := make(chan struct{})
	dispatcher := supervisorDispatcherFunc(func(ctx context.Context, lease app.Lease) (app.DispatchResult, error) {
		if lease.Generation != 1 || lease.WorkerID != "worker_supervisor" {
			return app.DispatchResult{}, errors.New("unexpected supervisor lease")
		}
		close(dispatchStarted)
		select {
		case <-finishDispatch:
			return app.DispatchResult{SubmissionKey: "mender.submit.run_supervisor.1"}, nil
		case <-ctx.Done():
			return app.DispatchResult{}, ctx.Err()
		}
	})
	supervisor, err := app.NewDispatchSupervisor(control, dispatcher, manualHeartbeatFactory{ticker: ticker}, supervisorConfig())
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		result app.DispatchResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, dispatchErr := supervisor.DispatchOne(context.Background(), "ws_supervisor", []string{"deploy_supervisor"})
		done <- outcome{result: result, err: dispatchErr}
	}()
	<-dispatchStarted
	ticker.ch <- time.Date(2026, 9, 10, 16, 0, 5, 0, time.UTC)
	<-control.heartbeatSignal
	close(finishDispatch)
	got := <-done
	if got.err != nil || got.result.SubmissionKey != "mender.submit.run_supervisor.1" {
		t.Fatal(got.result, got.err)
	}
	activate, leases, heartbeats, releases := control.counts()
	if activate != 1 || leases != 1 || heartbeats != 1 || releases != 0 {
		t.Fatal(activate, leases, heartbeats, releases)
	}
}

func TestDispatchSupervisorCancelsDispatcherOnHeartbeatLossWithoutRelease(t *testing.T) {
	ticker := &manualHeartbeatTicker{ch: make(chan time.Time, 1)}
	control := &supervisorControlFake{lease: supervisorLease(), heartbeatErr: app.ErrWorkerLeaseLost, heartbeatSignal: make(chan struct{}, 1)}
	dispatchStarted := make(chan struct{})
	dispatchCanceled := make(chan struct{})
	dispatcher := supervisorDispatcherFunc(func(ctx context.Context, _ app.Lease) (app.DispatchResult, error) {
		close(dispatchStarted)
		<-ctx.Done()
		close(dispatchCanceled)
		return app.DispatchResult{}, ctx.Err()
	})
	supervisor, err := app.NewDispatchSupervisor(control, dispatcher, manualHeartbeatFactory{ticker: ticker}, supervisorConfig())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, dispatchErr := supervisor.DispatchOne(context.Background(), "ws_supervisor", []string{"deploy_supervisor"})
		done <- dispatchErr
	}()
	<-dispatchStarted
	ticker.ch <- time.Date(2026, 9, 10, 16, 0, 5, 0, time.UTC)
	<-control.heartbeatSignal
	if err = <-done; !errors.Is(err, app.ErrWorkerLeaseLost) {
		t.Fatal(err)
	}
	<-dispatchCanceled
	_, _, heartbeats, releases := control.counts()
	if heartbeats != 1 || releases != 0 {
		t.Fatal(heartbeats, releases)
	}
}

func TestDispatchSupervisorReleasesLeaseWhenTickerCannotStart(t *testing.T) {
	control := &supervisorControlFake{lease: supervisorLease()}
	dispatchCalls := 0
	dispatcher := supervisorDispatcherFunc(func(context.Context, app.Lease) (app.DispatchResult, error) {
		dispatchCalls++
		return app.DispatchResult{}, nil
	})
	supervisor, err := app.NewDispatchSupervisor(control, dispatcher, manualHeartbeatFactory{err: errors.New("ticker unavailable")}, supervisorConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = supervisor.DispatchOne(context.Background(), "ws_supervisor", []string{"deploy_supervisor"}); !errors.Is(err, app.ErrSupervisorUnavailable) {
		t.Fatal(err)
	}
	_, _, heartbeats, releases := control.counts()
	if dispatchCalls != 0 || heartbeats != 0 || releases != 1 {
		t.Fatal(dispatchCalls, heartbeats, releases)
	}
}

func TestDispatchSupervisorValidatesConfigAndPropagatesNoWork(t *testing.T) {
	control := &supervisorControlFake{lease: supervisorLease(), leaseErr: app.ErrNoWork}
	dispatcher := supervisorDispatcherFunc(func(context.Context, app.Lease) (app.DispatchResult, error) {
		t.Fatal("dispatcher called without a lease")
		return app.DispatchResult{}, nil
	})
	ticker := &manualHeartbeatTicker{ch: make(chan time.Time, 1)}
	if supervisor, err := app.NewDispatchSupervisor(control, dispatcher, manualHeartbeatFactory{ticker: ticker}, app.SupervisorConfig{}); !errors.Is(err, app.ErrSupervisorUnavailable) || supervisor != nil {
		t.Fatal(supervisor, err)
	}
	supervisor, err := app.NewDispatchSupervisor(control, dispatcher, manualHeartbeatFactory{ticker: ticker}, supervisorConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = supervisor.DispatchOne(context.Background(), "ws_supervisor", []string{"deploy_supervisor"}); !errors.Is(err, app.ErrNoWork) {
		t.Fatal(err)
	}
}
