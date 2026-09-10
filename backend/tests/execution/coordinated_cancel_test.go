package execution_test

import (
	"context"
	"errors"
	app "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	"testing"
	"time"
)

type coordinatedFunc func(context.Context, ports.Caller, domain.RunID, string) (domain.Snapshot, bool, error)

func (f coordinatedFunc) CancelAdmission(c context.Context, p ports.Caller, id domain.RunID, r string) (domain.Snapshot, bool, error) {
	return f(c, p, id, r)
}
func TestManagedCancellationDoesNotFallBackOnErrorsOrInvalidReceipts(t *testing.T) {
	for _, mode := range []string{"success", "unsafe", "commit_unknown", "invalid_receipt"} {
		t.Run(mode, func(t *testing.T) {
			run := fixture(t, "ws_a")
			saved := 0
			repo := repositoryStub{find: func(context.Context, domain.WorkspaceID, domain.RunID) (domain.Run, error) { return run, nil }, save: func(context.Context, domain.Run, uint64) error { saved++; return nil }}
			var want error
			coordinator := coordinatedFunc(func(_ context.Context, _ ports.Caller, _ domain.RunID, _ string) (domain.Snapshot, bool, error) {
				s := run.Snapshot()
				s.State = domain.Canceled
				s.Version = 2
				s.UpdatedAt = s.CreatedAt.Add(time.Second)
				switch mode {
				case "unsafe":
					want = ports.ErrUnsafeCancel
				case "commit_unknown":
					want = ports.ErrCancelCommitUnconfirmed
				case "invalid_receipt":
					s.WorkspaceID = "ws_other"
					want = ports.ErrUnavailable
				}
				if mode == "unsafe" || mode == "commit_unknown" {
					return domain.Snapshot{}, false, want
				}
				return s, true, nil
			})
			s, e := app.NewServiceWithCancellation(repo, allow(), fixedClock{at.Add(time.Second)}, coordinator)
			if e != nil {
				t.Fatal(e)
			}
			v, e := s.CancelRun(context.Background(), caller, "run_1")
			if !errors.Is(e, want) || saved != 0 {
				t.Fatal(v, e, saved)
			}
			if mode == "success" && v.State != domain.Canceled {
				t.Fatal(v)
			}
		})
	}
}
func TestUnmanagedCancellationUsesLegacyCASAndDeniedCallerNeverCoordinates(t *testing.T) {
	run := fixture(t, "ws_a")
	saved, calls := 0, 0
	repo := repositoryStub{find: func(context.Context, domain.WorkspaceID, domain.RunID) (domain.Run, error) { return run, nil }, save: func(context.Context, domain.Run, uint64) error { saved++; return nil }}
	coord := coordinatedFunc(func(context.Context, ports.Caller, domain.RunID, string) (domain.Snapshot, bool, error) {
		calls++
		return domain.Snapshot{}, false, nil
	})
	s, e := app.NewServiceWithCancellation(repo, allow(), fixedClock{at.Add(time.Second)}, coord)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CancelRun(context.Background(), caller, "run_1"); e != nil || saved != 1 || calls != 1 {
		t.Fatal(e, saved, calls)
	}
	denied := authorizeFunc(func(context.Context, ports.Caller, ports.Action, domain.RunID) error { return ports.ErrForbidden })
	s, e = app.NewServiceWithCancellation(repo, denied, fixedClock{at}, coord)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CancelRun(context.Background(), caller, "run_1"); !errors.Is(e, ports.ErrForbidden) || calls != 1 || saved != 1 {
		t.Fatal(e, calls, saved)
	}
}
