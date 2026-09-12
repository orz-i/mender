package identitycancel

import (
	"context"
	"errors"

	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
)

type DelegatedAccess struct{ delegations identity.RunDelegations }

func NewDelegated(delegations identity.RunDelegations) *DelegatedAccess {
	return &DelegatedAccess{delegations: delegations}
}

func (a *DelegatedAccess) AuthorizeCancellation(ctx context.Context, caller application.Caller, _ string) error {
	if a == nil || a.delegations == nil {
		return application.ErrUnavailable
	}
	err := a.delegations.AuthorizeRunDelegation(ctx, identity.RunDelegationPrincipal{DelegationID: caller.CredentialID, WorkspaceID: caller.WorkspaceID, UserID: caller.SubjectID}, caller.WorkspaceID, "run:cancel")
	switch {
	case err == nil:
		return nil
	case errors.Is(err, identity.ErrUnauthenticated):
		return application.ErrCancelUnauthenticated
	case errors.Is(err, identity.ErrForbidden):
		return application.ErrForbidden
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return application.ErrUnavailable
	}
}

var _ application.CancelAuthorizer = (*DelegatedAccess)(nil)
