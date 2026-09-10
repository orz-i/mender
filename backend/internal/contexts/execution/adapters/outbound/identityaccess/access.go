package identityaccess

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
)

// Access is the anti-corruption layer. Only identity's public contract crosses contexts.
type Access struct{ identity identity.Identity }

func New(service identity.Identity) *Access { return &Access{identity: service} }
func translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, identity.ErrUnauthenticated):
		return ports.ErrUnauthenticated
	case errors.Is(err, identity.ErrForbidden):
		return ports.ErrForbidden
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return ports.ErrUnavailable
	}
}
func (a *Access) Authenticate(ctx context.Context, token string) (ports.Caller, error) {
	p, err := a.identity.Authenticate(ctx, token)
	return ports.Caller{SubjectID: p.SubjectID, WorkspaceID: domain.WorkspaceID(p.WorkspaceID), CredentialID: p.CredentialID}, translate(err)
}
func (a *Access) Authorize(ctx context.Context, c ports.Caller, action ports.Action, id domain.RunID) error {
	scope := string(action)
	switch action {
	case ports.ListRuns:
		if id != "" {
			return ports.ErrForbidden
		}
		scope = string(ports.ReadRun)
	case ports.ReadRunEvents:
		if !id.IsValid() {
			return ports.ErrForbidden
		}
		scope = string(ports.ReadRun)
	case ports.ReadRun, ports.CancelRun:
		if !id.IsValid() {
			return ports.ErrForbidden
		}
	default:
		return ports.ErrForbidden
	}
	return translate(a.identity.Authorize(ctx, identity.Principal{CredentialID: c.CredentialID, WorkspaceID: string(c.WorkspaceID), SubjectID: c.SubjectID}, string(c.WorkspaceID), scope))
}

var _ ports.Authenticator = (*Access)(nil)
var _ ports.Authorizer = (*Access)(nil)
