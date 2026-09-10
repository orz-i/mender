package postgres

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

type Repository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }
func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func (r *Repository) scoped(ctx context.Context, workspace domain.WorkspaceID) (pgx.Tx, error) {
	if !workspace.IsValid() {
		return nil, ports.ErrNotFound
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, ports.ErrUnavailable
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", string(workspace)); err != nil {
		rollback(tx)
		return nil, ports.ErrUnavailable
	}
	return tx, nil
}

func (r *Repository) Find(ctx context.Context, workspace domain.WorkspaceID, id domain.RunID) (domain.Run, error) {
	if err := ctx.Err(); err != nil {
		return domain.Run{}, err
	}
	if !id.IsValid() {
		return domain.Run{}, ports.ErrNotFound
	}
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return domain.Run{}, err
	}
	defer rollback(tx)
	var s domain.Snapshot
	var wid, rid, state string
	var version int64
	err = tx.QueryRow(ctx, "SELECT workspace_id,id,state,version,created_at,updated_at FROM execution.runs WHERE workspace_id=$1 AND id=$2", string(workspace), string(id)).Scan(&wid, &rid, &state, &version, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Run{}, ports.ErrNotFound
	}
	if err != nil || version < 1 {
		return domain.Run{}, ports.ErrUnavailable
	}
	s.ID = domain.RunID(rid)
	s.WorkspaceID = domain.WorkspaceID(wid)
	s.State = domain.State(state)
	s.Version = uint64(version)
	run, err := domain.Restore(s)
	if err != nil {
		return domain.Run{}, ports.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Run{}, ports.ErrUnavailable
	}
	return run, nil
}

// Save commits the CAS and actor-bearing event together. It never inserts a missing Run,
// contacts an upstream, releases funds or claims an external cancellation succeeded.
func (r *Repository) Save(ctx context.Context, run domain.Run, expected uint64, change ports.Change) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s := run.Snapshot()
	actor := change.Actor
	if actor.WorkspaceID != s.WorkspaceID || actor.SubjectID == "" || actor.CredentialID == "" {
		return ports.ErrForbidden
	}
	if expected == 0 || expected >= math.MaxInt64 || s.Version != expected+1 {
		return ports.ErrConflict
	}
	if _, err := domain.Restore(s); err != nil {
		return ports.ErrConflict
	}
	tx, err := r.scoped(ctx, s.WorkspaceID)
	if err != nil {
		return err
	}
	defer rollback(tx)
	// Legacy cancellation cannot strand a reservation or leave a dispatchable orphan.
	// A coordinated cancel/release/Job transition is a later use case; fail closed now.
	var managed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution.run_admissions WHERE workspace_id=$1 AND run_id=$2)`, string(s.WorkspaceID), string(s.ID)).Scan(&managed); err != nil {
		return ports.ErrUnavailable
	}
	if managed {
		return ports.ErrAdmissionManaged
	}
	tag, err := tx.Exec(ctx, `UPDATE execution.runs SET state=$1,version=$2,updated_at=$3
	 WHERE workspace_id=$4 AND id=$5 AND version=$6 AND created_at=$7 AND updated_at<=$3`, string(s.State), int64(s.Version), s.UpdatedAt.UTC().Truncate(time.Microsecond), string(s.WorkspaceID), string(s.ID), int64(expected), s.CreatedAt.UTC().Truncate(time.Microsecond))
	if err != nil {
		return ports.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return ports.ErrConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO execution.run_events(workspace_id,run_id,version,state,subject_id,credential_id,occurred_at,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, string(s.WorkspaceID), string(s.ID), int64(s.Version), string(s.State), actor.SubjectID, actor.CredentialID, s.UpdatedAt.UTC().Truncate(time.Microsecond), change.Reason); err != nil {
		return ports.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return ports.ErrUnavailable
	}
	return nil
}

var _ ports.Repository = (*Repository)(nil)
