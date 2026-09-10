package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
	"time"
)

type Factory func(pgx.Tx, string) (application.Scope, error)
type UnitOfWork struct {
	pool    *pgxpool.Pool
	factory Factory
}

func New(pool *pgxpool.Pool, f Factory) *UnitOfWork { return &UnitOfWork{pool, f} }
func (u *UnitOfWork) Within(ctx context.Context, w string, fn func(application.Scope) error) error {
	if !application.ValidID(w) || fn == nil || u.pool == nil || u.factory == nil {
		return application.ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	tx, e := u.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return application.ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, e = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, w); e != nil {
		return application.ErrUnavailable
	}
	scope, e := u.factory(tx, w)
	if e != nil {
		return application.ErrUnavailable
	}
	if e = fn(scope); e != nil {
		for _, allowed := range []error{application.ErrInvalid, application.ErrForbidden, application.ErrConflict, application.ErrBudgetExceeded, application.ErrBudgetUnavailable, context.Canceled, context.DeadlineExceeded} {
			if errors.Is(e, allowed) {
				return allowed
			}
		}
		return application.ErrUnavailable
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return application.ErrCommitUnconfirmed
	}
	return nil
}

var _ application.UnitOfWork = (*UnitOfWork)(nil)
