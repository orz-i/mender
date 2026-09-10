package coordinatedcancel

import (
	"context"
	"errors"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	pub "github.com/orz-i/mender/backend/internal/processes/admission/public"
)

type Access struct{ cancel pub.Canceler }

func New(c pub.Canceler) *Access { return &Access{c} }
func (a *Access) CancelAdmission(ctx context.Context, c ports.Caller, id domain.RunID, reason string) (domain.Snapshot, bool, error) {
	r, e := a.cancel.Cancel(ctx, pub.CancelCaller{WorkspaceID: string(c.WorkspaceID), SubjectID: c.SubjectID, CredentialID: c.CredentialID}, string(id), reason)
	if e != nil {
		switch {
		case errors.Is(e, context.Canceled), errors.Is(e, context.DeadlineExceeded):
		case errors.Is(e, pub.ErrCancelUnauthenticated):
			e = ports.ErrUnauthenticated
		case errors.Is(e, pub.ErrCancelForbidden):
			e = ports.ErrForbidden
		case errors.Is(e, pub.ErrCancelUnsafe):
			e = ports.ErrUnsafeCancel
		case errors.Is(e, pub.ErrCancelCommitUnconfirmed):
			e = ports.ErrCancelCommitUnconfirmed
		default:
			e = ports.ErrUnavailable
		}
		return domain.Snapshot{}, false, e
	}
	return domain.Snapshot{ID: domain.RunID(r.Run.RunID), WorkspaceID: domain.WorkspaceID(r.Run.WorkspaceID), State: domain.State(r.Run.State), Version: r.Run.Version, CreatedAt: r.Run.CreatedAt, UpdatedAt: r.Run.UpdatedAt}, r.Found, nil
}

var _ ports.CoordinatedCanceler = (*Access)(nil)
