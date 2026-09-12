package identityaccess

import (
	"context"

	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
)

type Delegated struct{ delegations identity.RunDelegations }

func NewDelegated(delegations identity.RunDelegations) *Delegated {
	return &Delegated{delegations: delegations}
}

func (d *Delegated) AuthenticateRunCost(ctx context.Context, raw string) (application.RunCostActor, error) {
	if d == nil || d.delegations == nil {
		return application.RunCostActor{}, application.ErrObservabilityUnavailable
	}
	principal, err := d.delegations.AuthenticateRunDelegation(ctx, raw)
	if err != nil {
		return application.RunCostActor{}, mapError(err)
	}
	return application.RunCostActor{WorkspaceID: principal.WorkspaceID, SubjectID: principal.UserID, CredentialID: principal.DelegationID}, nil
}

func (d *Delegated) AuthorizeRunCost(ctx context.Context, actor application.RunCostActor, workspace, _ string, action string) error {
	if d == nil || d.delegations == nil {
		return application.ErrObservabilityUnavailable
	}
	principal := identity.RunDelegationPrincipal{WorkspaceID: actor.WorkspaceID, UserID: actor.SubjectID, DelegationID: actor.CredentialID}
	return mapError(d.delegations.AuthorizeRunDelegation(ctx, principal, workspace, action))
}

var _ application.RunCostAuthorizer = (*Delegated)(nil)
