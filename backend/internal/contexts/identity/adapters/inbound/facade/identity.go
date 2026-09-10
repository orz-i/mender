package facade

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/identity/application"
	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
)

type Identity struct{ service *application.Service }

func New(service *application.Service) *Identity { return &Identity{service: service} }

func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, application.ErrUnauthenticated):
		return identity.ErrUnauthenticated
	case errors.Is(err, application.ErrForbidden):
		return identity.ErrForbidden
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return identity.ErrUnavailable
	}
}

func (f *Identity) Authenticate(ctx context.Context, token string) (identity.Principal, error) {
	p, err := f.service.Authenticate(ctx, token)
	return identity.Principal{CredentialID: p.CredentialID, WorkspaceID: p.WorkspaceID, SubjectID: p.SubjectID}, mapError(err)
}
func (f *Identity) Authorize(ctx context.Context, p identity.Principal, workspace, action string) error {
	return mapError(f.service.Authorize(ctx, application.Principal{CredentialID: p.CredentialID, WorkspaceID: p.WorkspaceID, SubjectID: p.SubjectID}, workspace, action))
}

var _ identity.Identity = (*Identity)(nil)
