package cancellation

import (
	"context"
	"errors"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
	pub "github.com/orz-i/mender/backend/internal/processes/admission/public"
)

type Facade struct{ service *application.Cancellation }

func New(s *application.Cancellation) *Facade { return &Facade{s} }
func (f *Facade) Cancel(ctx context.Context, c pub.CancelCaller, id, reason string) (pub.CancelResult, error) {
	r, e := f.service.Cancel(ctx, application.Caller(c), id, reason)
	if e != nil {
		switch {
		case errors.Is(e, context.Canceled), errors.Is(e, context.DeadlineExceeded):
			return pub.CancelResult{}, e
		case errors.Is(e, application.ErrInvalid):
			e = pub.ErrCancelInvalid
		case errors.Is(e, application.ErrForbidden):
			e = pub.ErrCancelForbidden
		case errors.Is(e, application.ErrCancelUnauthenticated):
			e = pub.ErrCancelUnauthenticated
		case errors.Is(e, application.ErrCancelUnsafe):
			e = pub.ErrCancelUnsafe
		case errors.Is(e, application.ErrCancelCommitUnconfirmed):
			e = pub.ErrCancelCommitUnconfirmed
		default:
			e = pub.ErrCancelUnavailable
		}
		return pub.CancelResult{}, e
	}
	return pub.CancelResult{Found: r.Found, Run: pub.CancelState(r.Run)}, nil
}

var _ pub.Canceler = (*Facade)(nil)
