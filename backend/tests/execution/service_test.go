package execution_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type authorizeFunc func(context.Context, ports.Caller, ports.Action, domain.RunID) error

func (f authorizeFunc) Authorize(ctx context.Context, caller ports.Caller, action ports.Action, id domain.RunID) error {
	return f(ctx, caller, action, id)
}

type repositoryStub struct {
	find func(context.Context, domain.WorkspaceID, domain.RunID) (domain.Run, error)
	save func(context.Context, domain.Run, uint64) error
}

func (r repositoryStub) Find(ctx context.Context, workspace domain.WorkspaceID, id domain.RunID) (domain.Run, error) {
	return r.find(ctx, workspace, id)
}
func (r repositoryStub) Save(ctx context.Context, run domain.Run, revision uint64, _ ports.Change) error {
	return r.save(ctx, run, revision)
}

func allow() authorizeFunc {
	return func(ctx context.Context, caller ports.Caller, _ ports.Action, _ domain.RunID) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if caller.SubjectID != "subject_a" || caller.WorkspaceID != "ws_a" {
			return ports.ErrForbidden
		}
		return nil
	}
}

func service(t *testing.T, repo ports.Repository, auth ports.Authorizer) *application.Service {
	t.Helper()
	s, err := application.NewService(repo, auth, fixedClock{at.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var caller = ports.Caller{SubjectID: "subject_a", WorkspaceID: "ws_a"}

func TestGetAndCancelWithInjectedPorts(t *testing.T) {
	ctx := context.Background()
	repo := memory(fixture(t, "ws_a"))
	var actions []ports.Action
	policy := authorizeFunc(func(ctx context.Context, who ports.Caller, action ports.Action, id domain.RunID) error {
		actions = append(actions, action)
		return allow()(ctx, who, action, id)
	})
	s := service(t, repo, policy)
	initial, err := s.GetRun(ctx, caller, "run_1")
	if err != nil || initial.State != domain.Queued {
		t.Fatal(initial, err)
	}
	canceled, err := s.CancelRun(ctx, caller, "run_1")
	if err != nil || canceled.State != domain.Canceled || canceled.Version != 2 {
		t.Fatal(canceled, err)
	}
	repeated, err := s.CancelRun(ctx, caller, "run_1")
	if err != nil || repeated != canceled {
		t.Fatal("idempotent cancel changed result", err)
	}
	if len(actions) != 3 || actions[0] != ports.ReadRun || actions[1] != ports.CancelRun || actions[2] != ports.CancelRun {
		t.Fatal("every request must authorize its actual operation", actions)
	}
}

func TestDeniedAndUnavailablePolicyNeverReadStorage(t *testing.T) {
	for _, denied := range []error{ports.ErrForbidden, errors.New("policy unavailable")} {
		s := service(t, repositoryStub{}, authorizeFunc(func(context.Context, ports.Caller, ports.Action, domain.RunID) error { return denied }))
		if _, err := s.GetRun(context.Background(), caller, "run_1"); !errors.Is(err, denied) {
			t.Fatal(err)
		}
		if _, err := s.CancelRun(context.Background(), caller, "run_1"); !errors.Is(err, denied) {
			t.Fatal(err)
		}
	}
}

func TestInvalidCallerAndCanceledRequestFailBeforePolicy(t *testing.T) {
	s := service(t, repositoryStub{}, authorizeFunc(func(context.Context, ports.Caller, ports.Action, domain.RunID) error {
		t.Fatal("policy should not be called")
		return nil
	}))
	for _, who := range []ports.Caller{{}, {SubjectID: " ", WorkspaceID: "ws_a"}, {SubjectID: "subject_a", WorkspaceID: "../ws_b"}} {
		if _, err := s.GetRun(context.Background(), who, "run_1"); !errors.Is(err, application.ErrInvalidRequest) {
			t.Fatal(err)
		}
	}
	if _, err := s.GetRun(context.Background(), caller, "run/1"); !errors.Is(err, application.ErrInvalidRequest) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.GetRun(ctx, caller, "run_1"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRepositoryCannotReturnAnotherTenantsRun(t *testing.T) {
	wrong := fixture(t, "ws_b")
	repo := repositoryStub{find: func(context.Context, domain.WorkspaceID, domain.RunID) (domain.Run, error) { return wrong, nil }}
	s := service(t, repo, allow())
	if _, err := s.GetRun(context.Background(), caller, "run_1"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("incorrect adapter leaked another tenant", err)
	}
	if _, err := s.CancelRun(context.Background(), caller, "run_1"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestRunningCancellationRemainsIntentAndReconciliationDoesNotWrite(t *testing.T) {
	run := fixture(t, "ws_a")
	if err := run.Start(at); err != nil {
		t.Fatal(err)
	}
	repo := memory(run)
	s := service(t, repo, allow())
	v, err := s.CancelRun(context.Background(), caller, "run_1")
	if err != nil || v.State != domain.CancelRequested {
		t.Fatal("running operation was reported canceled", v, err)
	}
	if err := run.MarkOutcomeUnconfirmed(at); err != nil {
		t.Fatal(err)
	}
	s = service(t, repositoryStub{find: func(context.Context, domain.WorkspaceID, domain.RunID) (domain.Run, error) { return run, nil }}, allow())
	if _, err := s.CancelRun(context.Background(), caller, "run_1"); !errors.Is(err, domain.ErrOutcomeUnconfirmed) {
		t.Fatal(err)
	}
}

func TestCancellationCannotOverwriteConcurrentWorkerStart(t *testing.T) {
	ctx := context.Background()
	run := fixture(t, "ws_a")
	repo := memory(run)
	var writes int
	s := service(t, repositoryStub{
		find: repo.Find,
		save: func(ctx context.Context, canceled domain.Run, expected uint64) error {
			writes++
			worker, err := repo.Find(ctx, "ws_a", "run_1")
			if err != nil {
				return err
			}
			if err := worker.Start(at); err != nil {
				return err
			}
			if err := repo.Save(ctx, worker, 1, ports.Change{Actor: caller}); err != nil {
				return err
			}
			return repo.Save(ctx, canceled, expected, ports.Change{Actor: caller})
		},
	}, allow())
	if _, err := s.CancelRun(ctx, caller, "run_1"); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("stale cancellation overwrote worker state", err)
	}
	stored, err := repo.Find(ctx, "ws_a", "run_1")
	if err != nil || stored.Snapshot().State != domain.Running || writes != 1 {
		t.Fatal("conflict must not be blindly retried", err)
	}
}

func TestTerminalReadAndCancellationDoNotRewriteStorage(t *testing.T) {
	run := fixture(t, "ws_a")
	if err := run.Start(at); err != nil {
		t.Fatal(err)
	}
	if err := run.ConfirmSucceeded(at); err != nil {
		t.Fatal(err)
	}
	s := service(t, repositoryStub{find: func(context.Context, domain.WorkspaceID, domain.RunID) (domain.Run, error) { return run, nil }}, allow())
	v, err := s.CancelRun(context.Background(), caller, "run_1")
	if err != nil || v.State != domain.Succeeded || v.Version != 3 {
		t.Fatal(v, err)
	}
}

func TestCancellationAndStorageFailureAreNotReportedAsSuccess(t *testing.T) {
	run := fixture(t, "ws_a")
	broken := errors.New("storage unavailable")
	s := service(t, repositoryStub{find: func(context.Context, domain.WorkspaceID, domain.RunID) (domain.Run, error) { return run, nil }, save: func(context.Context, domain.Run, uint64) error { return broken }}, allow())
	if _, err := s.CancelRun(context.Background(), caller, "run_1"); !errors.Is(err, broken) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s = service(t, repositoryStub{}, authorizeFunc(func(context.Context, ports.Caller, ports.Action, domain.RunID) error { cancel(); return nil }))
	if _, err := s.GetRun(ctx, caller, "run_1"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestMissingPortsAreRejected(t *testing.T) {
	for _, deps := range []struct {
		repo  ports.Repository
		auth  ports.Authorizer
		clock ports.Clock
	}{
		{nil, allow(), fixedClock{at}}, {memory(), nil, fixedClock{at}}, {memory(), allow(), nil},
	} {
		if _, err := application.NewService(deps.repo, deps.auth, deps.clock); !errors.Is(err, application.ErrMissingDependency) {
			t.Fatal(err)
		}
	}
}
