package application

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrCancelUnsafe = errors.New("run or reservation cannot be safely canceled")
var ErrCancelUnauthenticated = errors.New("cancellation credential inactive")
var ErrCancelCommitUnconfirmed = errors.New("cancellation commit unconfirmed; retry the same Run")

type CancelRef struct {
	WorkspaceID, RunID, ReservationID, BudgetID, PeriodID, Currency string
	AmountMicro                                                     int64
}
type CancelChange struct {
	SubjectID, CredentialID, Reason string
	At                              time.Time
}
type CancelState struct {
	WorkspaceID, RunID, State string
	Version                   uint64
	CreatedAt, UpdatedAt      time.Time
	Replayed                  bool
}
type ReleaseState struct {
	Released   bool
	ReleasedAt time.Time
}
type CancelResult struct {
	Found bool
	Run   CancelState
}
type CancelAuthorizer interface {
	AuthorizeCancellation(context.Context, Caller, string) error
}
type CancelScope interface {
	FindReference(context.Context, string) (CancelRef, bool, error)
	InspectReservation(context.Context, CancelRef) (ReleaseState, error)
	InspectRun(context.Context, CancelRef) (CancelState, error)
	Release(context.Context, CancelRef, time.Time) error
	CancelRun(context.Context, CancelRef, CancelChange) (CancelState, error)
}
type CancelUnitOfWork interface {
	WithinCancel(context.Context, string, func(CancelScope) error) error
}
type Cancellation struct {
	auth  CancelAuthorizer
	clock Clock
	uow   CancelUnitOfWork
}

func NewCancellation(auth CancelAuthorizer, clock Clock, uow CancelUnitOfWork) (*Cancellation, error) {
	if auth == nil || clock == nil || uow == nil {
		return nil, ErrUnavailable
	}
	return &Cancellation{auth, clock, uow}, nil
}
func (s *Cancellation) Cancel(ctx context.Context, c Caller, id, reason string) (CancelResult, error) {
	if e := ctx.Err(); e != nil {
		return CancelResult{}, e
	}
	if !ValidID(c.WorkspaceID) || !ValidID(c.SubjectID) || !ValidID(c.CredentialID) || !ValidID(id) || !utf8.ValidString(reason) || strings.ContainsRune(reason, 0) || len([]rune(reason)) > 500 {
		return CancelResult{}, ErrInvalid
	}
	if e := s.auth.AuthorizeCancellation(ctx, c, id); e != nil {
		return CancelResult{}, e
	}
	var result CancelResult
	e := s.uow.WithinCancel(ctx, c.WorkspaceID, func(tx CancelScope) error {
		ref, found, err := tx.FindReference(ctx, id)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		if ref.WorkspaceID != c.WorkspaceID || ref.RunID != id || !ValidID(ref.ReservationID) || !ValidID(ref.BudgetID) || !ValidID(ref.PeriodID) || len(ref.Currency) != 3 || strings.Trim(ref.Currency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" || ref.AmountMicro < 0 {
			return ErrCancelUnsafe
		}
		// Locks: original budget period -> reservation -> Run -> Job -> admitted Outbox.
		release, err := tx.InspectReservation(ctx, ref)
		if err != nil {
			return err
		}
		run, err := tx.InspectRun(ctx, ref)
		if err != nil {
			return err
		}
		if run.WorkspaceID != c.WorkspaceID || run.RunID != id || run.CreatedAt.IsZero() || run.UpdatedAt.Before(run.CreatedAt) {
			return ErrCancelUnsafe
		}
		// Recheck after waiting on row locks, including idempotent repeats.
		if err = s.auth.AuthorizeCancellation(ctx, c, id); err != nil {
			return err
		}
		if run.Replayed {
			if run.State != "canceled" || run.Version != 2 || !release.Released || !release.ReleasedAt.Equal(run.UpdatedAt) {
				return ErrCancelUnsafe
			}
			result = CancelResult{true, run}
			return nil
		}
		if release.Released || run.State != "queued" || run.Version != 1 {
			return ErrCancelUnsafe
		}
		at := s.clock.Now().UTC().Truncate(time.Microsecond)
		if at.IsZero() || at.Year() > 9999 || at.Before(run.UpdatedAt) {
			return ErrCancelUnsafe
		}
		if err = tx.Release(ctx, ref, at); err != nil {
			return err
		}
		next, err := tx.CancelRun(ctx, ref, CancelChange{c.SubjectID, c.CredentialID, reason, at})
		if err != nil {
			return err
		}
		if next.WorkspaceID != ref.WorkspaceID || next.RunID != ref.RunID || next.State != "canceled" || next.Version != 2 || next.Replayed || !next.CreatedAt.Equal(run.CreatedAt) || !next.UpdatedAt.Equal(at) {
			return ErrCancelUnsafe
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		result = CancelResult{true, next}
		return nil
	})
	if e != nil {
		return CancelResult{}, e
	}
	return result, nil
}
