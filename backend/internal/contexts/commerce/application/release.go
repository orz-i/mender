package application

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/commerce/domain"
)

var ErrReleaseConflict = errors.New("reservation cannot be released in its current state")

type ReleaseRequest struct {
	WorkspaceID, RunID, ReservationID, BudgetID, PeriodID, Currency string
	AmountMicro                                                     int64
}
type ReleaseState struct {
	Released   bool
	ReleasedAt time.Time
}
type LockedRelease struct {
	Reference             ReleaseRequest
	State                 string
	CreatedAt, ReleasedAt time.Time
	Allowance             domain.Allowance
}

// Lock always acquires the budget-period row before its reservation. All methods
// use one transaction; releasing an expired/inactive original period is permitted.
type ReleaseRepository interface {
	LockRelease(context.Context, ReleaseRequest) (LockedRelease, error)
	StoreRelease(context.Context, ReleaseRequest, domain.Allowance, int64, time.Time) error
}
type ReleaseService struct{ repo ReleaseRepository }

func NewReleaseService(repo ReleaseRepository) (*ReleaseService, error) {
	if repo == nil {
		return nil, ErrReservationUnavailable
	}
	return &ReleaseService{repo: repo}, nil
}
func (s *ReleaseService) load(ctx context.Context, q ReleaseRequest) (LockedRelease, error) {
	if err := ctx.Err(); err != nil {
		return LockedRelease{}, err
	}
	if q.WorkspaceID == "" || q.RunID == "" || q.ReservationID == "" || q.BudgetID == "" || q.PeriodID == "" || len(q.Currency) != 3 || q.AmountMicro < 0 {
		return LockedRelease{}, ErrReleaseConflict
	}
	r, err := s.repo.LockRelease(ctx, q)
	if err != nil {
		return r, err
	}
	if r.Reference != q || r.CreatedAt.IsZero() {
		return r, ErrReleaseConflict
	}
	if _, err = domain.RestoreAllowance(r.Allowance.Snapshot()); err != nil {
		return r, ErrReleaseConflict
	}
	switch r.State {
	case "held":
		if !r.ReleasedAt.IsZero() || r.Allowance.Snapshot().Reserved < q.AmountMicro {
			return r, ErrReleaseConflict
		}
	case "released":
		if r.ReleasedAt.IsZero() || r.ReleasedAt.Before(r.CreatedAt) {
			return r, ErrReleaseConflict
		}
	default:
		return r, ErrReleaseConflict
	}
	return r, nil
}
func (s *ReleaseService) Inspect(ctx context.Context, q ReleaseRequest) (ReleaseState, error) {
	r, e := s.load(ctx, q)
	return ReleaseState{Released: r.State == "released", ReleasedAt: r.ReleasedAt}, e
}
func (s *ReleaseService) Release(ctx context.Context, q ReleaseRequest, at time.Time) error {
	r, e := s.load(ctx, q)
	if e != nil {
		return e
	}
	// Idempotent replay is decided by the coordinator after checking execution facts.
	if r.State != "held" || at.IsZero() || at.Before(r.CreatedAt) || at.Year() > 9999 {
		return ErrReleaseConflict
	}
	expected := r.Allowance.Snapshot().Revision
	if e = r.Allowance.Release(q.AmountMicro); e != nil {
		return ErrReleaseConflict
	}
	return s.repo.StoreRelease(ctx, q, r.Allowance, expected, at.UTC().Truncate(time.Microsecond))
}
