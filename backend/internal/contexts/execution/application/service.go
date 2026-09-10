package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

var (
	ErrInvalidRequest     = errors.New("invalid run request")
	ErrMissingDependency  = errors.New("run service requires repository, authorizer and clock")
	ErrOutcomeUnconfirmed = domain.ErrOutcomeUnconfirmed
)

// View is local use-case output. Transport DTOs and integration events are mapped elsewhere.
type View struct {
	ID          domain.RunID
	WorkspaceID domain.WorkspaceID
	State       domain.State
	Version     uint64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewServiceWithCancellation(repository ports.Repository, authorizer ports.Authorizer, clock ports.Clock, coordinator ports.CoordinatedCanceler) (*Service, error) {
	if coordinator == nil {
		return nil, ErrMissingDependency
	}
	s, err := NewService(repository, authorizer, clock)
	if err != nil {
		return nil, err
	}
	s.coordinator = coordinator
	return s, nil
}

type Service struct {
	repository  ports.Repository
	authorizer  ports.Authorizer
	clock       ports.Clock
	coordinator ports.CoordinatedCanceler
}

func NewService(repository ports.Repository, authorizer ports.Authorizer, clock ports.Clock) (*Service, error) {
	if repository == nil || authorizer == nil || clock == nil {
		return nil, ErrMissingDependency
	}
	return &Service{repository: repository, authorizer: authorizer, clock: clock}, nil
}

func view(run domain.Run) View {
	s := run.Snapshot()
	return View{ID: s.ID, WorkspaceID: s.WorkspaceID, State: s.State, Version: s.Version, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
}

func (s *Service) load(ctx context.Context, caller ports.Caller, id domain.RunID, action ports.Action) (domain.Run, error) {
	if err := ctx.Err(); err != nil {
		return domain.Run{}, err
	}
	if strings.TrimSpace(caller.SubjectID) == "" || len(caller.SubjectID) > 512 || !caller.WorkspaceID.IsValid() || !id.IsValid() {
		return domain.Run{}, ErrInvalidRequest
	}
	// Authorization precedes lookup so denied callers cannot probe resource existence.
	if err := s.authorizer.Authorize(ctx, caller, action, id); err != nil {
		return domain.Run{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.Run{}, err
	}
	run, err := s.repository.Find(ctx, caller.WorkspaceID, id)
	if err != nil {
		return domain.Run{}, err
	}
	snapshot := run.Snapshot()
	if snapshot.Version == 0 || snapshot.ID != id || snapshot.WorkspaceID != caller.WorkspaceID {
		return domain.Run{}, ports.ErrNotFound
	}
	return run, nil
}

func (s *Service) GetRun(ctx context.Context, caller ports.Caller, id domain.RunID) (View, error) {
	run, err := s.load(ctx, caller, id, ports.ReadRun)
	if err != nil {
		return View{}, err
	}
	return view(run), nil
}

// CancelRun delegates admission-managed runs to the explicitly composed atomic
// cancel/release coordinator. The legacy path only persists local state or intent.
// Neither path sends an upstream request or implies payment settlement/refunds.
func (s *Service) CancelRun(ctx context.Context, caller ports.Caller, id domain.RunID) (View, error) {
	return s.CancelRunWithReason(ctx, caller, id, "")
}

func (s *Service) CancelRunWithReason(ctx context.Context, caller ports.Caller, id domain.RunID, reason string) (View, error) {
	if len([]rune(reason)) > 500 || strings.ContainsRune(reason, 0) {
		return View{}, ErrInvalidRequest
	}
	run, err := s.load(ctx, caller, id, ports.CancelRun)
	if err != nil {
		return View{}, err
	}
	if s.coordinator != nil {
		snapshot, managed, e := s.coordinator.CancelAdmission(ctx, caller, id, reason)
		if e != nil {
			return View{}, e
		}
		if managed {
			if snapshot.ID != id || snapshot.WorkspaceID != caller.WorkspaceID || snapshot.State != domain.Canceled {
				return View{}, ports.ErrUnavailable
			}
			current, e := domain.Restore(snapshot)
			if e != nil {
				return View{}, ports.ErrUnavailable
			}
			return view(current), nil
		}
	}
	expected := run.Snapshot().Version
	changed, err := run.RequestCancel(s.clock.Now())
	if err != nil {
		return View{}, err
	}
	if changed {
		if err := ctx.Err(); err != nil {
			return View{}, err
		}
		if err := s.repository.Save(ctx, run, expected, ports.Change{Actor: caller, Reason: reason}); err != nil {
			return View{}, err
		}
	}
	return view(run), nil
}
