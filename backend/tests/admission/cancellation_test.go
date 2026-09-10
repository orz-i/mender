package admission_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	app "github.com/orz-i/mender/backend/internal/processes/admission/application"
)

// These ports test use-case ordering only. Real rollback is exercised separately
// by tests/integration/cancellation_test.go against owned PostgreSQL resources.
type cancelAuthFunc func(context.Context, app.Caller, string) error

func (f cancelAuthFunc) AuthorizeCancellation(c context.Context, who app.Caller, id string) error {
	return f(c, who, id)
}

type cancelClock struct{ at time.Time }

func (c cancelClock) Now() time.Time { return c.at }

type cancelUOW struct {
	scope     *cancelScopeFake
	entered   int
	commitErr error
}

func (u *cancelUOW) WithinCancel(ctx context.Context, w string, fn func(app.CancelScope) error) error {
	u.entered++
	if w != "ws_cancel" {
		return app.ErrForbidden
	}
	if e := fn(u.scope); e != nil {
		return e
	}
	return u.commitErr
}

type cancelScopeFake struct {
	ref          app.CancelRef
	release      app.ReleaseState
	run          app.CancelState
	found        bool
	failAt       string
	failure      error
	afterInspect func()
	next         func(app.CancelState) app.CancelState
	calls        []string
}

func (s *cancelScopeFake) step(name string) error {
	s.calls = append(s.calls, name)
	if s.failAt == name {
		return s.failure
	}
	return nil
}
func (s *cancelScopeFake) FindReference(_ context.Context, _ string) (app.CancelRef, bool, error) {
	return s.ref, s.found, s.step("reference")
}
func (s *cancelScopeFake) InspectReservation(_ context.Context, _ app.CancelRef) (app.ReleaseState, error) {
	return s.release, s.step("reservation")
}
func (s *cancelScopeFake) InspectRun(_ context.Context, _ app.CancelRef) (app.CancelState, error) {
	e := s.step("run")
	if s.afterInspect != nil {
		s.afterInspect()
	}
	return s.run, e
}
func (s *cancelScopeFake) Release(_ context.Context, _ app.CancelRef, _ time.Time) error {
	return s.step("release")
}
func (s *cancelScopeFake) CancelRun(_ context.Context, _ app.CancelRef, ch app.CancelChange) (app.CancelState, error) {
	e := s.step("cancel")
	n := s.run
	n.State, n.Version, n.UpdatedAt, n.Replayed = "canceled", 2, ch.At, false
	if s.next != nil {
		n = s.next(n)
	}
	return n, e
}
func cancelFixture() (*cancelScopeFake, app.Caller, time.Time) {
	at := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	s := &cancelScopeFake{
		found: true,
		ref:   app.CancelRef{WorkspaceID: "ws_cancel", RunID: "run_cancel", ReservationID: "res_cancel", BudgetID: "budget_a", PeriodID: "p1", Currency: "USD", AmountMicro: 60},
		run:   app.CancelState{WorkspaceID: "ws_cancel", RunID: "run_cancel", State: "queued", Version: 1, CreatedAt: at, UpdatedAt: at},
	}
	return s, app.Caller{WorkspaceID: "ws_cancel", SubjectID: "subject_a", CredentialID: "key_a"}, at.Add(time.Second)
}
func newCancel(t *testing.T, s *cancelScopeFake, at time.Time, auth cancelAuthFunc) (*app.Cancellation, *cancelUOW) {
	t.Helper()
	u := &cancelUOW{scope: s}
	svc, e := app.NewCancellation(auth, cancelClock{at}, u)
	if e != nil {
		t.Fatal(e)
	}
	return svc, u
}
func TestCancellationOrdersOwnersAndRechecksAuthorization(t *testing.T) {
	s, who, at := cancelFixture()
	n := 0
	auth := cancelAuthFunc(func(ctx context.Context, c app.Caller, id string) error {
		n++
		if c != who || id != s.ref.RunID {
			t.Fatal("changed principal or target")
		}
		if n == 1 && len(s.calls) != 0 {
			t.Fatal("read before permission")
		}
		if n == 2 && !reflect.DeepEqual(s.calls, []string{"reference", "reservation", "run"}) {
			t.Fatal("permission not rechecked after locks", s.calls)
		}
		return ctx.Err()
	})
	svc, u := newCancel(t, s, at, auth)
	r, e := svc.Cancel(context.Background(), who, s.ref.RunID, "reason")
	if e != nil || !r.Found || r.Run.State != "canceled" || r.Run.Version != 2 || n != 2 || u.entered != 1 {
		t.Fatal(r, e, n)
	}
	if !reflect.DeepEqual(s.calls, []string{"reference", "reservation", "run", "release", "cancel"}) {
		t.Fatal(s.calls)
	}
}
func TestCancellationRejectsInvalidInputBeforeAuthorityOrStorage(t *testing.T) {
	for _, reason := range []string{strings.Repeat("中", 501), "bad\x00reason", string([]byte{0xff})} {
		s, who, at := cancelFixture()
		svc, u := newCancel(t, s, at, func(context.Context, app.Caller, string) error {
			t.Fatal("invalid input reached authority")
			return nil
		})
		if _, e := svc.Cancel(context.Background(), who, s.ref.RunID, reason); !errors.Is(e, app.ErrInvalid) || u.entered != 0 {
			t.Fatal(e)
		}
	}
	s, who, at := cancelFixture()
	svc, u := newCancel(t, s, at, func(context.Context, app.Caller, string) error {
		t.Fatal("canceled request reached authority")
		return nil
	})
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if _, e := svc.Cancel(ctx, who, s.ref.RunID, ""); !errors.Is(e, context.Canceled) || u.entered != 0 {
		t.Fatal(e)
	}
}
func TestCancellationDenialAndRevocationCannotRelease(t *testing.T) {
	for _, denyAt := range []int{1, 2} {
		s, who, at := cancelFixture()
		n := 0
		svc, u := newCancel(t, s, at, func(context.Context, app.Caller, string) error {
			n++
			if n == denyAt {
				return app.ErrCancelUnauthenticated
			}
			return nil
		})
		r, e := svc.Cancel(context.Background(), who, s.ref.RunID, "")
		if !errors.Is(e, app.ErrCancelUnauthenticated) || r.Found {
			t.Fatal(r, e)
		}
		if denyAt == 1 && u.entered != 0 {
			t.Fatal("denied before storage")
		}
		for _, c := range s.calls {
			if c == "release" || c == "cancel" {
				t.Fatal("revoked request wrote", s.calls)
			}
		}
	}
}
func TestCancellationReplayRequiresMatchingReleaseProof(t *testing.T) {
	s, who, at := cancelFixture()
	s.run.State, s.run.Version, s.run.UpdatedAt, s.run.Replayed = "canceled", 2, at, true
	s.release = app.ReleaseState{Released: true, ReleasedAt: at}
	svc, _ := newCancel(t, s, at.Add(-time.Hour), func(context.Context, app.Caller, string) error { return nil })
	r, e := svc.Cancel(context.Background(), who, s.ref.RunID, "new reason")
	if e != nil || !r.Run.Replayed || len(s.calls) != 3 {
		t.Fatal(r, e, s.calls)
	}
	s.release.ReleasedAt = at.Add(time.Microsecond)
	s.calls = nil
	if r, e = svc.Cancel(context.Background(), who, s.ref.RunID, ""); !errors.Is(e, app.ErrCancelUnsafe) || r.Found || len(s.calls) != 3 {
		t.Fatal(r, e, s.calls)
	}
}
func TestCancellationRejectsUnsafeSnapshotsWithoutWrites(t *testing.T) {
	cases := []struct {
		name   string
		change func(*cancelScopeFake)
		clock  func(time.Time) time.Time
	}{
		{"other_tenant", func(s *cancelScopeFake) { s.ref.WorkspaceID = "ws_other" }, nil},
		{"other_run", func(s *cancelScopeFake) { s.run.RunID = "run_other" }, nil},
		{"running", func(s *cancelScopeFake) { s.run.State = "running"; s.run.Version = 2 }, nil},
		{"reconciling", func(s *cancelScopeFake) { s.run.State = "reconciling"; s.run.Version = 2 }, nil},
		{"released_without_receipt", func(s *cancelScopeFake) { s.release.Released = true }, nil},
		{"clock_backwards", func(*cancelScopeFake) {}, func(at time.Time) time.Time { return at.Add(-time.Hour) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, who, at := cancelFixture()
			tc.change(s)
			if tc.clock != nil {
				at = tc.clock(at)
			}
			svc, _ := newCancel(t, s, at, func(context.Context, app.Caller, string) error { return nil })
			r, e := svc.Cancel(context.Background(), who, "run_cancel", "")
			if !errors.Is(e, app.ErrCancelUnsafe) || r.Found {
				t.Fatal(r, e)
			}
			for _, c := range s.calls {
				if c == "release" || c == "cancel" {
					t.Fatal(s.calls)
				}
			}
		})
	}
}
func TestCancellationNeverReturnsReceiptOnWriteOrCommitError(t *testing.T) {
	for _, step := range []string{"reference", "reservation", "run", "release", "cancel", "commit"} {
		t.Run(step, func(t *testing.T) {
			s, who, at := cancelFixture()
			s.failAt = step
			s.failure = errors.New("fixture failure")
			svc, u := newCancel(t, s, at, func(context.Context, app.Caller, string) error { return nil })
			want := s.failure
			if step == "commit" {
				u.commitErr = app.ErrCancelCommitUnconfirmed
				want = u.commitErr
			}
			r, e := svc.Cancel(context.Background(), who, s.ref.RunID, "")
			if !errors.Is(e, want) || r != (app.CancelResult{}) {
				t.Fatal(r, e)
			}
		})
	}
}
func TestCancellationMissingReferenceAndInvalidOwnerResult(t *testing.T) {
	s, who, at := cancelFixture()
	s.found = false
	svc, _ := newCancel(t, s, at, func(context.Context, app.Caller, string) error { return nil })
	r, e := svc.Cancel(context.Background(), who, s.ref.RunID, "")
	if e != nil || r.Found || !reflect.DeepEqual(s.calls, []string{"reference"}) {
		t.Fatal(r, e, s.calls)
	}
	s.found = true
	s.next = func(v app.CancelState) app.CancelState { v.UpdatedAt = at.Add(time.Second); return v }
	if r, e = svc.Cancel(context.Background(), who, s.ref.RunID, ""); !errors.Is(e, app.ErrCancelUnsafe) || r.Found {
		t.Fatal(r, e)
	}
}
