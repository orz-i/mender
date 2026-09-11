package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/processes/settlement/application"
)

type Factory func(pgx.Tx, string) (application.Scope, error)
type UnitOfWork struct {
	pool    *pgxpool.Pool
	factory Factory
}

func New(pool *pgxpool.Pool, f Factory) *UnitOfWork { return &UnitOfWork{pool: pool, factory: f} }
func (u *UnitOfWork) Within(ctx context.Context, workspace string, fn func(application.Scope) error) error {
	if u == nil || u.pool == nil || u.factory == nil || fn == nil {
		return application.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := u.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return application.ErrUnavailable
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(c)
	}()
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace); err != nil {
		return application.ErrUnavailable
	}
	scope, err := u.factory(tx, workspace)
	if err != nil {
		return application.ErrUnavailable
	}
	if err = fn(scope); err != nil {
		for _, allowed := range []error{application.ErrInvalid, application.ErrNoWork, context.Canceled, context.DeadlineExceeded} {
			if errors.Is(err, allowed) {
				return allowed
			}
		}
		return application.ErrUnavailable
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ErrUnavailable
	}
	return nil
}

var _ application.UnitOfWork = (*UnitOfWork)(nil)
