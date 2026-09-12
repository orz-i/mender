package identityaccess

import (
	"context"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
)

// DelegatedAccess is intentionally separate from machine Access. It is only
// composed for the Console delegated Run surface, so a browser session cookie
// or delegated token never silently becomes a machine credential.
type DelegatedAccess struct{ delegations identity.RunDelegations }

func NewDelegated(delegations identity.RunDelegations) *DelegatedAccess {
	return &DelegatedAccess{delegations: delegations}
}

func (a *DelegatedAccess) Authenticate(ctx context.Context, token string) (ports.Caller, error) {
	if a == nil || a.delegations == nil {
		return ports.Caller{}, ports.ErrUnavailable
	}
	p, err := a.delegations.AuthenticateRunDelegation(ctx, token)
	return ports.Caller{SubjectID: p.UserID, WorkspaceID: domain.WorkspaceID(p.WorkspaceID), CredentialID: p.DelegationID}, translate(err)
}

func (a *DelegatedAccess) Authorize(ctx context.Context, caller ports.Caller, action ports.Action, id domain.RunID) error {
	if a == nil || a.delegations == nil {
		return ports.ErrUnavailable
	}
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
	return translate(a.delegations.AuthorizeRunDelegation(ctx, identity.RunDelegationPrincipal{DelegationID: caller.CredentialID, WorkspaceID: string(caller.WorkspaceID), UserID: caller.SubjectID}, string(caller.WorkspaceID), scope))
}

var _ ports.Authenticator = (*DelegatedAccess)(nil)
var _ ports.Authorizer = (*DelegatedAccess)(nil)
