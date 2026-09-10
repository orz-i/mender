package application

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

var (
	ErrWorkerUnavailable = errors.New("worker control storage unavailable")
	ErrNoWork            = errors.New("no execution job available")
	ErrWorkerLeaseLost   = errors.New("worker lease lost")
	ErrInvalidWorker     = errors.New("invalid worker control request")
)

type WorkerRepository interface {
	ActivateOne(context.Context, domain.WorkspaceID, []string, time.Time) (bool, error)
	Acquire(context.Context, domain.WorkspaceID, string, time.Time, time.Time) (domain.JobSnapshot, error)
	Renew(context.Context, domain.LeaseToken, time.Time, time.Time) (domain.JobSnapshot, error)
	ReleaseBeforeSubmit(context.Context, domain.LeaseToken, time.Time, time.Time) (domain.JobSnapshot, error)
	RecoverExpired(context.Context, domain.WorkspaceID, time.Time) (domain.JobSnapshot, bool, error)
}

type WorkerClock interface{ Now() time.Time }

type Lease struct {
	WorkspaceID domain.WorkspaceID
	RunID       domain.RunID
	WorkerID    string
	Generation  uint64
	AttemptNo   uint32
	LeaseUntil  time.Time
}

func (l Lease) Token() domain.LeaseToken {
	return domain.LeaseToken{WorkspaceID: l.WorkspaceID, RunID: l.RunID, WorkerID: l.WorkerID, Generation: l.Generation}
}

type WorkerControl struct {
	repo  WorkerRepository
	clock WorkerClock
}

func NewWorkerControl(repo WorkerRepository, clock WorkerClock) (*WorkerControl, error) {
	if repo == nil || clock == nil {
		return nil, ErrWorkerUnavailable
	}
	return &WorkerControl{repo: repo, clock: clock}, nil
}

func validControlID(v string) bool {
	if len(v) == 0 || len(v) > 128 {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func normalizeWorkerError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrLeaseLost), errors.Is(err, domain.ErrLeaseExpired):
		return ErrWorkerLeaseLost
	case errors.Is(err, domain.ErrInvalidJob), errors.Is(err, domain.ErrInvalidJobTime), errors.Is(err, domain.ErrInvalidWorker), errors.Is(err, domain.ErrJobState), errors.Is(err, domain.ErrAttemptLimit):
		return ErrInvalidWorker
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, ErrNoWork):
		return err
	default:
		return ErrWorkerUnavailable
	}
}

func now(clock WorkerClock) (time.Time, error) {
	at := clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() || at.Year() < 1 || at.Year() > 9999 {
		return time.Time{}, ErrWorkerUnavailable
	}
	return at, nil
}

func (s *WorkerControl) Activate(ctx context.Context, workspace domain.WorkspaceID, revisions []string, limit int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !workspace.IsValid() || len(revisions) == 0 || len(revisions) > 64 || limit < 1 || limit > 100 {
		return 0, ErrInvalidWorker
	}
	seen := map[string]bool{}
	for _, revision := range revisions {
		if !validControlID(revision) || seen[revision] {
			return 0, ErrInvalidWorker
		}
		seen[revision] = true
	}
	at, err := now(s.clock)
	if err != nil {
		return 0, err
	}
	count := 0
	for count < limit {
		changed, e := s.repo.ActivateOne(ctx, workspace, revisions, at)
		if e != nil {
			return count, normalizeWorkerError(e)
		}
		if !changed {
			break
		}
		count++
	}
	return count, nil
}

func leaseFromSnapshot(s domain.JobSnapshot) (Lease, error) {
	if _, err := domain.RestoreJob(s); err != nil || s.State != domain.JobLeased || s.LeaseGeneration == 0 || s.AttemptCount == 0 {
		return Lease{}, ErrWorkerUnavailable
	}
	return Lease{WorkspaceID: s.WorkspaceID, RunID: s.RunID, WorkerID: s.LeaseOwner, Generation: s.LeaseGeneration, AttemptNo: s.AttemptCount, LeaseUntil: s.LeaseUntil}, nil
}

func (s *WorkerControl) LeaseOne(ctx context.Context, workspace domain.WorkspaceID, worker string, ttl time.Duration) (Lease, error) {
	if err := ctx.Err(); err != nil {
		return Lease{}, err
	}
	if !workspace.IsValid() || !validControlID(worker) || ttl < 5*time.Second || ttl > 5*time.Minute {
		return Lease{}, ErrInvalidWorker
	}
	at, err := now(s.clock)
	if err != nil {
		return Lease{}, err
	}
	job, err := s.repo.Acquire(ctx, workspace, worker, at, at.Add(ttl))
	if err != nil {
		return Lease{}, normalizeWorkerError(err)
	}
	return leaseFromSnapshot(job)
}

func (s *WorkerControl) Heartbeat(ctx context.Context, lease Lease, ttl time.Duration) (Lease, error) {
	if err := ctx.Err(); err != nil {
		return Lease{}, err
	}
	if ttl < 5*time.Second || ttl > 5*time.Minute || lease.Generation == 0 || lease.AttemptNo == 0 || uint64(lease.AttemptNo) != lease.Generation {
		return Lease{}, ErrInvalidWorker
	}
	at, err := now(s.clock)
	if err != nil {
		return Lease{}, err
	}
	job, err := s.repo.Renew(ctx, lease.Token(), at, at.Add(ttl))
	if err != nil {
		return Lease{}, normalizeWorkerError(err)
	}
	return leaseFromSnapshot(job)
}

func (s *WorkerControl) ReleaseBeforeSubmit(ctx context.Context, lease Lease, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay < 0 || delay > 10*time.Minute || lease.Generation == 0 || lease.AttemptNo == 0 || uint64(lease.AttemptNo) != lease.Generation {
		return ErrInvalidWorker
	}
	at, err := now(s.clock)
	if err != nil {
		return err
	}
	_, err = s.repo.ReleaseBeforeSubmit(ctx, lease.Token(), at, at.Add(delay))
	return normalizeWorkerError(err)
}

func (s *WorkerControl) RecoverExpired(ctx context.Context, workspace domain.WorkspaceID, limit int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !workspace.IsValid() || limit < 1 || limit > 100 {
		return 0, ErrInvalidWorker
	}
	at, err := now(s.clock)
	if err != nil {
		return 0, err
	}
	count := 0
	for count < limit {
		_, found, e := s.repo.RecoverExpired(ctx, workspace, at)
		if e != nil {
			return count, normalizeWorkerError(e)
		}
		if !found {
			break
		}
		count++
	}
	return count, nil
}
