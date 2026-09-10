package admission

import (
	"context"
	"errors"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	pub "github.com/orz-i/mender/backend/internal/contexts/execution/public"
)

type Cancellations struct {
	service *application.CancellationService
}

func NewCancellations(s *application.CancellationService) *Cancellations { return &Cancellations{s} }
func cancelError(e error) error {
	if e == nil {
		return nil
	}
	if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
		return e
	}
	if errors.Is(e, application.ErrUnsafeCancellation) {
		return pub.ErrUnsafeCancellation
	}
	return pub.ErrCancellationUnavailable
}
func (f *Cancellations) FindReference(ctx context.Context, w, id string) (pub.CancellationRef, bool, error) {
	r, b, e := f.service.FindReference(ctx, w, id)
	return pub.CancellationRef(r), b, cancelError(e)
}
func (f *Cancellations) Inspect(ctx context.Context, q pub.CancellationRef) (pub.CancellationState, error) {
	r, e := f.service.Inspect(ctx, application.CancellationRef(q))
	return pub.CancellationState(r), cancelError(e)
}
func (f *Cancellations) Cancel(ctx context.Context, q pub.CancellationRef, ch pub.CancellationChange) (pub.CancellationState, error) {
	r, e := f.service.Cancel(ctx, application.CancellationRef(q), application.CancellationChange(ch))
	return pub.CancellationState(r), cancelError(e)
}

var _ pub.Cancellation = (*Cancellations)(nil)
