package admission_test

import (
	"context"
	"errors"
	"testing"
	"time"

	catalog "github.com/orz-i/mender/backend/internal/contexts/catalog/public"
	commerce "github.com/orz-i/mender/backend/internal/contexts/commerce/public"
	connections "github.com/orz-i/mender/backend/internal/contexts/connections/public"
	distribution "github.com/orz-i/mender/backend/internal/contexts/distribution/public"
	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
	"github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/capabilities"
	identityaccess "github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/identityaccess"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
)

type toolsetsStub struct {
	binding distribution.Binding
	err     error
	calls   int
}

func (s *toolsetsStub) ResolveBinding(context.Context, string, string, string, string) (distribution.Binding, error) {
	s.calls++
	return s.binding, s.err
}

func (s *toolsetsStub) ListDirectBindings(context.Context, string, string) ([]distribution.Binding, error) {
	return nil, distribution.ErrNotFound
}

type catalogStub struct {
	tool  catalog.ToolVersion
	err   error
	calls int
}

func (s *catalogStub) ResolveToolVersion(context.Context, string, string, string) (catalog.ToolVersion, error) {
	s.calls++
	return s.tool, s.err
}

type connectionsStub struct {
	access connections.Access
	err    error
	calls  int
}

func (s *connectionsStub) ResolveAccess(context.Context, string, string, string, string, time.Time) (connections.Access, error) {
	s.calls++
	return s.access, s.err
}

type pricingStub struct {
	terms commerce.Terms
	err   error
	calls int
}

func (s *pricingStub) ResolveAdmissionTerms(context.Context, string, string, string, string, string, time.Time) (commerce.Terms, error) {
	s.calls++
	return s.terms, s.err
}

func TestProductionResolverUsesOwnedPublishedFactsAndTightestExpiry(t *testing.T) {
	toolsets := &toolsetsStub{binding: distribution.Binding{ToolsetVersionID: "set_v1", ToolVersionID: "tool_v1", BudgetID: "budget_a"}}
	catalogPort := &catalogStub{tool: catalog.ToolVersion{ID: "tool_v1", ToolID: "tool_a", Version: "1.0.0", ProviderID: "provider_a", PriceVersionID: "price_v1", DeploymentRevision: "deploy_v1"}}
	connectionsPort := &connectionsStub{access: connections.Access{ConnectionID: "conn_a", ProviderID: "provider_a", Revision: 3, ValidUntil: moment.Add(30 * time.Minute)}}
	pricing := &pricingStub{terms: commerce.Terms{PriceVersionID: "price_v1", BudgetID: "budget_a", PeriodID: "period_a", Currency: "USD", ReserveMicro: 50, ValidUntil: moment.Add(time.Hour)}}
	resolver, err := capabilities.NewResolver(toolsets, catalogPort, connectionsPort, pricing, clock{moment})
	if err != nil {
		t.Fatal(err)
	}
	p, err := resolver.Resolve(context.Background(), who, request(), `{"n":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if p.ToolVersionID != "tool_v1" || p.BudgetID != "budget_a" || p.PeriodID != "period_a" || p.PriceVersionID != "price_v1" || p.DeploymentRevision != "deploy_v1" || p.ReserveMicro != 50 || !p.ValidUntil.Equal(moment.Add(30*time.Minute)) {
		t.Fatal("incorrect immutable plan", p)
	}
}

func TestProductionResolverFailsClosedBeforeLaterCapabilities(t *testing.T) {
	q := request()
	toolsets := &toolsetsStub{err: distribution.ErrNotFound}
	catalogPort := &catalogStub{}
	connectionsPort := &connectionsStub{}
	pricing := &pricingStub{}
	resolver, err := capabilities.NewResolver(toolsets, catalogPort, connectionsPort, pricing, clock{moment})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = resolver.Resolve(context.Background(), who, q, `{}`); !errors.Is(err, application.ErrForbidden) || catalogPort.calls != 0 || connectionsPort.calls != 0 || pricing.calls != 0 {
		t.Fatal("unbound tool leaked into later resolution", err, catalogPort.calls, connectionsPort.calls, pricing.calls)
	}
	toolsets.err = nil
	toolsets.binding = distribution.Binding{ToolsetVersionID: q.ToolsetVersionID, ToolVersionID: "tool_v1", BudgetID: "budget_a"}
	catalogPort.err = catalog.ErrNotFound
	if _, err = resolver.Resolve(context.Background(), who, q, `{}`); !errors.Is(err, application.ErrForbidden) || connectionsPort.calls != 0 || pricing.calls != 0 {
		t.Fatal("disabled catalog version reached later resolution", err, connectionsPort.calls, pricing.calls)
	}
	catalogPort.err = nil
	catalogPort.tool = catalog.ToolVersion{ID: "tool_v1", ToolID: q.ToolID, Version: q.ToolVersion, ProviderID: "provider_a", PriceVersionID: "price_v1", DeploymentRevision: "deploy_v1"}
	connectionsPort.err = connections.ErrForbidden
	if _, err = resolver.Resolve(context.Background(), who, q, `{}`); !errors.Is(err, application.ErrForbidden) || pricing.calls != 0 {
		t.Fatal("denied connection reached pricing", err, pricing.calls)
	}
	connectionsPort.err = nil
	connectionsPort.access = connections.Access{ConnectionID: q.ConnectionID, ProviderID: "provider_a", Revision: 1, ValidUntil: moment.Add(time.Hour)}
	pricing.err = commerce.ErrPriceUnavailable
	if _, err = resolver.Resolve(context.Background(), who, q, `{}`); !errors.Is(err, application.ErrForbidden) {
		t.Fatal("unavailable price was reported as infrastructure failure", err)
	}
}

type identityStub struct {
	action            string
	authErr, allowErr error
}

func (s *identityStub) Authenticate(context.Context, string) (identity.Principal, error) {
	if s.authErr != nil {
		return identity.Principal{}, s.authErr
	}
	return identity.Principal{CredentialID: "key_a", WorkspaceID: "ws_a", SubjectID: "sa_a"}, nil
}

func (s *identityStub) Authorize(_ context.Context, _ identity.Principal, _ string, action string) error {
	s.action = action
	return s.allowErr
}

func TestAdmissionIdentityAdapterRequiresRunCreate(t *testing.T) {
	identityPort := &identityStub{}
	access := identityaccess.New(identityPort)
	caller, err := access.Authenticate(context.Background(), "opaque")
	if err != nil || caller != who {
		t.Fatal(caller, err)
	}
	if err = access.Authorize(context.Background(), caller, request()); err != nil || identityPort.action != "run:create" {
		t.Fatal("wrong creation scope", identityPort.action, err)
	}
	identityPort.allowErr = identity.ErrForbidden
	if err = access.Authorize(context.Background(), caller, request()); !errors.Is(err, application.ErrForbidden) {
		t.Fatal(err)
	}
}
