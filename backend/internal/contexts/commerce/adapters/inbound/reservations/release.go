package reservations

import (
	"context"
	"errors"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	pub "github.com/orz-i/mender/backend/internal/contexts/commerce/public"
	"time"
)

type Releases struct{ service *application.ReleaseService }

func NewReleases(s *application.ReleaseService) *Releases { return &Releases{s} }
func releaseError(e error) error {
	if e == nil {
		return nil
	}
	if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
		return e
	}
	if errors.Is(e, application.ErrReleaseConflict) {
		return pub.ErrReleaseConflict
	}
	return pub.ErrReleaseUnavailable
}
func (f *Releases) Inspect(ctx context.Context, q pub.ReleaseRequest) (pub.ReleaseState, error) {
	r, e := f.service.Inspect(ctx, application.ReleaseRequest(q))
	return pub.ReleaseState(r), releaseError(e)
}
func (f *Releases) Release(ctx context.Context, q pub.ReleaseRequest, at time.Time) error {
	return releaseError(f.service.Release(ctx, application.ReleaseRequest(q), at))
}

var _ pub.Releaser = (*Releases)(nil)
