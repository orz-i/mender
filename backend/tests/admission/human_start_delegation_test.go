package admission_test

import (
	"context"
	"errors"
	"testing"

	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
	identitystart "github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/identitystart"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
)

type startIdentity struct {
	principal identity.RunStartDelegationPrincipal
}

func (s startIdentity) AuthenticateRunStartDelegation(context.Context, string) (identity.RunStartDelegationPrincipal, error) {
	return s.principal, nil
}

func (s startIdentity) AuthorizeRunStartDelegation(_ context.Context, principal identity.RunStartDelegationPrincipal, workspace string, requested identity.RunStartConstraint) error {
	if principal != s.principal || workspace != s.principal.WorkspaceID {
		return identity.ErrForbidden
	}
	bound := principal.Constraint
	if requested.ToolsetVersionID != bound.ToolsetVersionID || requested.ToolID != bound.ToolID || requested.ToolVersion != bound.ToolVersion || requested.ToolVersionID != bound.ToolVersionID || requested.ConnectionID != bound.ConnectionID || requested.Currency != bound.Currency || requested.IdempotencyKey != bound.IdempotencyKey || requested.MaxChargeMicro > bound.MaxChargeMicro {
		return identity.ErrForbidden
	}
	return nil
}

func TestHumanStartAdmissionACLDoesNotBecomeGeneralRunCreate(t *testing.T) {
	bound := identity.RunStartConstraint{ToolsetVersionID: "set_alpha_v1", ToolID: "tool_alpha", ToolVersion: "1.0.0", ToolVersionID: "tool_alpha_v1", ConnectionID: "conn_alpha", Currency: "USD", MaxChargeMicro: 75, IdempotencyKey: "human-alpha-0001"}
	principal := identity.RunStartDelegationPrincipal{DelegationID: "rsd_alpha", WorkspaceID: "ws_alpha", UserID: "user_alpha", Constraint: bound}
	access := identitystart.New(startIdentity{principal: principal})
	caller, err := access.Authenticate(context.Background(), "opaque-token")
	if err != nil || caller.Start == nil || caller.CredentialID != "rsd_alpha" || caller.SubjectID != "user_alpha" {
		t.Fatal("delegated admission caller is invalid", caller, err)
	}
	request := application.Request{IdempotencyKey: bound.IdempotencyKey, ToolsetVersionID: bound.ToolsetVersionID, ToolID: bound.ToolID, ToolVersion: bound.ToolVersion, ConnectionID: bound.ConnectionID, Currency: bound.Currency, MaxChargeMicro: "75"}
	if err = access.Authorize(context.Background(), caller, request); err != nil {
		t.Fatal("exact delegated request denied", err)
	}
	request.MaxChargeMicro = "74"
	if err = access.Authorize(context.Background(), caller, request); err != nil {
		t.Fatal("lower cap denied", err)
	}
	request.MaxChargeMicro = "76"
	if err = access.Authorize(context.Background(), caller, request); !errors.Is(err, application.ErrForbidden) {
		t.Fatal("delegation raised its own fee cap", err)
	}
	request.MaxChargeMicro = "75"
	request.ConnectionID = "conn_other"
	if err = access.Authorize(context.Background(), caller, request); !errors.Is(err, application.ErrForbidden) {
		t.Fatal("delegation changed Connection", err)
	}
	request.ConnectionID = "conn_alpha"
	request.IdempotencyKey = "human-alpha-0002"
	if err = access.Authorize(context.Background(), caller, request); !errors.Is(err, application.ErrForbidden) {
		t.Fatal("delegation authorized a second logical Run", err)
	}
}
