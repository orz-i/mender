package identitystart

import (
	"context"
	"errors"
	"strconv"

	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
)

type Delegated struct{ delegations identity.RunStartDelegations }

func New(delegations identity.RunStartDelegations) *Delegated {
	return &Delegated{delegations: delegations}
}

func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, identity.ErrUnauthenticated):
		return application.ErrUnauthenticated
	case errors.Is(err, identity.ErrForbidden):
		return application.ErrForbidden
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return application.ErrUnavailable
	}
}

func applicationConstraint(value identity.RunStartConstraint) application.StartConstraint {
	return application.StartConstraint{
		ToolsetVersionID: value.ToolsetVersionID,
		ToolID:           value.ToolID,
		ToolVersion:      value.ToolVersion,
		ToolVersionID:    value.ToolVersionID,
		ConnectionID:     value.ConnectionID,
		Currency:         value.Currency,
		MaxChargeMicro:   value.MaxChargeMicro,
		IdempotencyKey:   value.IdempotencyKey,
	}
}

func publicConstraint(value application.StartConstraint) identity.RunStartConstraint {
	return identity.RunStartConstraint{
		ToolsetVersionID: value.ToolsetVersionID,
		ToolID:           value.ToolID,
		ToolVersion:      value.ToolVersion,
		ToolVersionID:    value.ToolVersionID,
		ConnectionID:     value.ConnectionID,
		Currency:         value.Currency,
		MaxChargeMicro:   value.MaxChargeMicro,
		IdempotencyKey:   value.IdempotencyKey,
	}
}

func (d *Delegated) Authenticate(ctx context.Context, token string) (application.Caller, error) {
	if d == nil || d.delegations == nil {
		return application.Caller{}, application.ErrUnavailable
	}
	principal, err := d.delegations.AuthenticateRunStartDelegation(ctx, token)
	if err != nil {
		return application.Caller{}, mapError(err)
	}
	constraint := applicationConstraint(principal.Constraint)
	return application.Caller{WorkspaceID: principal.WorkspaceID, SubjectID: principal.UserID, CredentialID: principal.DelegationID, Start: &constraint}, nil
}

func strictCap(raw string) (int64, error) {
	if raw == "" || len(raw) > 19 || len(raw) > 1 && raw[0] == '0' {
		return 0, application.ErrInvalid
	}
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0, application.ErrInvalid
		}
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, application.ErrInvalid
	}
	return value, nil
}

func (d *Delegated) Authorize(ctx context.Context, caller application.Caller, request application.Request) error {
	if d == nil || d.delegations == nil || caller.Start == nil {
		return application.ErrForbidden
	}
	cap, err := strictCap(request.MaxChargeMicro)
	if err != nil {
		return err
	}
	principal := identity.RunStartDelegationPrincipal{
		DelegationID: caller.CredentialID,
		WorkspaceID:  caller.WorkspaceID,
		UserID:       caller.SubjectID,
		Constraint:   publicConstraint(*caller.Start),
	}
	requested := identity.RunStartConstraint{
		ToolsetVersionID: request.ToolsetVersionID,
		ToolID:           request.ToolID,
		ToolVersion:      request.ToolVersion,
		ToolVersionID:    caller.Start.ToolVersionID,
		ConnectionID:     request.ConnectionID,
		Currency:         request.Currency,
		MaxChargeMicro:   cap,
		IdempotencyKey:   request.IdempotencyKey,
	}
	return mapError(d.delegations.AuthorizeRunStartDelegation(ctx, principal, caller.WorkspaceID, requested))
}

var _ application.Authenticator = (*Delegated)(nil)
var _ application.Authorizer = (*Delegated)(nil)
