package identityaccess

import (
	"context"
	"errors"

	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
	"github.com/orz-i/mender/backend/internal/processes/mcpbridge/application"
)

type Access struct{ identity identity.Identity }

func New(service identity.Identity) *Access { return &Access{identity: service} }

func (a *Access) Authenticate(ctx context.Context, token string) (application.Caller, error) {
	if a == nil || a.identity == nil {
		return application.Caller{}, application.ErrUnavailable
	}
	p, err := a.identity.Authenticate(ctx, token)
	if err != nil {
		switch {
		case errors.Is(err, identity.ErrUnauthenticated):
			return application.Caller{}, application.ErrUnauthenticated
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return application.Caller{}, err
		default:
			return application.Caller{}, application.ErrUnavailable
		}
	}
	return application.Caller{WorkspaceID: p.WorkspaceID, SubjectID: p.SubjectID, CredentialID: p.CredentialID}, nil
}

var _ application.Authenticator = (*Access)(nil)
