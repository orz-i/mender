package execution_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

// This adapter exists only in _test.go. Neither API nor Worker can link it.
// It is not durable storage, a production database adapter or a distributed lock.
type memoryRepository struct {
	mu   sync.Mutex
	runs map[runKey]domain.Run
}

type runKey struct {
	workspace domain.WorkspaceID
	run       domain.RunID
}

func memory(runs ...domain.Run) *memoryRepository {
	repo := &memoryRepository{runs: make(map[runKey]domain.Run)}
	for _, run := range runs {
		s := run.Snapshot()
		repo.runs[runKey{s.WorkspaceID, s.ID}] = run
	}
	return repo
}

func (r *memoryRepository) Find(ctx context.Context, workspace domain.WorkspaceID, id domain.RunID) (domain.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.Run{}, err
	}
	run, ok := r.runs[runKey{workspace, id}]
	if !ok {
		return domain.Run{}, ports.ErrNotFound
	}
	return run, nil
}

func (r *memoryRepository) Save(ctx context.Context, run domain.Run, expected uint64, _ ports.Change) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	next := run.Snapshot()
	key := runKey{next.WorkspaceID, next.ID}
	current, ok := r.runs[key]
	if !ok {
		return ports.ErrNotFound
	}
	old := current.Snapshot()
	if old.Version != expected || expected == ^uint64(0) || next.Version != expected+1 || !old.CreatedAt.Equal(next.CreatedAt) || next.UpdatedAt.Before(old.UpdatedAt) {
		return ports.ErrConflict
	}
	r.runs[key] = run
	return nil
}

var _ ports.Repository = (*memoryRepository)(nil)
var at = time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)

func fixture(t *testing.T, workspace domain.WorkspaceID) domain.Run {
	t.Helper()
	run, err := domain.NewQueuedRun("run_1", workspace, at)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestRepositoryTenantIsolationAndDetachedValues(t *testing.T) {
	ctx := context.Background()
	repo := memory(fixture(t, "ws_a"), fixture(t, "ws_b"))
	if _, err := repo.Find(ctx, "ws_missing", "run_1"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	run, err := repo.Find(ctx, "ws_a", "run_1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run.RequestCancel(at); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Find(ctx, "ws_a", "run_1")
	if err != nil || stored.Snapshot().State != domain.Queued {
		t.Fatal("Find leaked mutable storage", err)
	}
	if err := repo.Save(ctx, run, 1, ports.Change{Actor: caller}); err != nil {
		t.Fatal(err)
	}
	other, err := repo.Find(ctx, "ws_b", "run_1")
	if err != nil || other.Snapshot().State != domain.Queued {
		t.Fatal("save changed another workspace", err)
	}
}

func TestRepositoryCompareAndSwapHasOneWinner(t *testing.T) {
	run := fixture(t, "ws_a")
	repo := memory(run)
	if _, err := run.RequestCancel(at); err != nil {
		t.Fatal(err)
	}
	var wins, conflicts, failures atomic.Int32
	var group sync.WaitGroup
	for range 32 {
		group.Add(1)
		go func() {
			defer group.Done()
			err := repo.Save(context.Background(), run, 1, ports.Change{Actor: caller})
			switch {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, ports.ErrConflict):
				conflicts.Add(1)
			default:
				failures.Add(1)
			}
		}()
	}
	group.Wait()
	if wins.Load() != 1 || conflicts.Load() != 31 || failures.Load() != 0 {
		t.Fatalf("wins=%d conflicts=%d failures=%d", wins.Load(), conflicts.Load(), failures.Load())
	}
}

func TestRepositoryRejectsCanceledContextAndInvalidRevision(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run := fixture(t, "ws_a")
	repo := memory(run)
	if _, err := repo.Find(ctx, "ws_a", "run_1"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := run.RequestCancel(at); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, run, 1, ports.Change{Actor: caller}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), run, 99, ports.Change{Actor: caller}); !errors.Is(err, ports.ErrConflict) {
		t.Fatal(err)
	}
	stored, err := repo.Find(context.Background(), "ws_a", "run_1")
	if err != nil || stored.Snapshot().Version != 1 {
		t.Fatal("rejected write mutated store")
	}
}
