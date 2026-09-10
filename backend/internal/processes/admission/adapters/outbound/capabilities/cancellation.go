package capabilities

import (
	"context"
	"errors"
	c "github.com/orz-i/mender/backend/internal/contexts/commerce/public"
	e "github.com/orz-i/mender/backend/internal/contexts/execution/public"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
	"time"
)

type CancelScope struct {
	workspace string
	commerce  c.Releaser
	execution e.Cancellation
}

func NewCancelScope(w string, commerce c.Releaser, execution e.Cancellation) *CancelScope {
	return &CancelScope{w, commerce, execution}
}
func cancelErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, c.ErrReleaseConflict) || errors.Is(err, e.ErrUnsafeCancellation) {
		return application.ErrCancelUnsafe
	}
	return application.ErrUnavailable
}
func (s *CancelScope) FindReference(ctx context.Context, id string) (application.CancelRef, bool, error) {
	r, b, err := s.execution.FindReference(ctx, s.workspace, id)
	return application.CancelRef(r), b, cancelErr(err)
}
func (s *CancelScope) InspectReservation(ctx context.Context, q application.CancelRef) (application.ReleaseState, error) {
	if q.WorkspaceID != s.workspace {
		return application.ReleaseState{}, application.ErrForbidden
	}
	r, err := s.commerce.Inspect(ctx, c.ReleaseRequest(q))
	return application.ReleaseState(r), cancelErr(err)
}
func (s *CancelScope) InspectRun(ctx context.Context, q application.CancelRef) (application.CancelState, error) {
	if q.WorkspaceID != s.workspace {
		return application.CancelState{}, application.ErrForbidden
	}
	r, err := s.execution.Inspect(ctx, e.CancellationRef(q))
	return application.CancelState(r), cancelErr(err)
}
func (s *CancelScope) Release(ctx context.Context, q application.CancelRef, at time.Time) error {
	if q.WorkspaceID != s.workspace {
		return application.ErrForbidden
	}
	return cancelErr(s.commerce.Release(ctx, c.ReleaseRequest(q), at))
}
func (s *CancelScope) CancelRun(ctx context.Context, q application.CancelRef, ch application.CancelChange) (application.CancelState, error) {
	if q.WorkspaceID != s.workspace {
		return application.CancelState{}, application.ErrForbidden
	}
	r, err := s.execution.Cancel(ctx, e.CancellationRef(q), e.CancellationChange(ch))
	return application.CancelState(r), cancelErr(err)
}

var _ application.CancelScope = (*CancelScope)(nil)
