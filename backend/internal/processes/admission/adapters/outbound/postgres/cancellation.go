package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
	"time"
)

type CancelFactory func(pgx.Tx, string) (application.CancelScope, error)
type CancelUnitOfWork struct {
	pool    *pgxpool.Pool
	factory CancelFactory
}

func NewCancelUnitOfWork(p *pgxpool.Pool, f CancelFactory) *CancelUnitOfWork {
	return &CancelUnitOfWork{p, f}
}
func (u *CancelUnitOfWork) WithinCancel(ctx context.Context, w string, fn func(application.CancelScope) error) error {
	if !application.ValidID(w) || u.pool == nil || u.factory == nil || fn == nil {
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
		c, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = tx.Rollback(c)
	}()
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, w); err != nil {
		return application.ErrUnavailable
	}
	s, err := u.factory(tx, w)
	if err != nil {
		return application.ErrUnavailable
	}
	if err = fn(s); err != nil {
		for _, known := range []error{application.ErrInvalid, application.ErrForbidden, application.ErrCancelUnauthenticated, application.ErrCancelUnsafe, context.Canceled, context.DeadlineExceeded} {
			if errors.Is(err, known) {
				return known
			}
		}
		return application.ErrUnavailable
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ErrCancelCommitUnconfirmed
	}
	return nil
}

var _ application.CancelUnitOfWork = (*CancelUnitOfWork)(nil)
