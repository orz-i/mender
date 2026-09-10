package capabilities

import (
	"context"
	"errors"
	"time"

	catalog "github.com/orz-i/mender/backend/internal/contexts/catalog/public"
	commerce "github.com/orz-i/mender/backend/internal/contexts/commerce/public"
	connections "github.com/orz-i/mender/backend/internal/contexts/connections/public"
	distribution "github.com/orz-i/mender/backend/internal/contexts/distribution/public"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
)

type Resolver struct {
	toolsets    distribution.Toolsets
	catalog     catalog.Catalog
	connections connections.Connections
	pricing     commerce.Pricing
	clock       application.Clock
}

func NewResolver(toolsets distribution.Toolsets, catalogPort catalog.Catalog, connectionsPort connections.Connections, pricing commerce.Pricing, clock application.Clock) (*Resolver, error) {
	if toolsets == nil || catalogPort == nil || connectionsPort == nil || pricing == nil || clock == nil {
		return nil, application.ErrUnavailable
	}
	return &Resolver{toolsets: toolsets, catalog: catalogPort, connections: connectionsPort, pricing: pricing, clock: clock}, nil
}

func mapContext(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func (r *Resolver) Resolve(ctx context.Context, caller application.Caller, q application.Request, _ string) (application.Plan, error) {
	if err := ctx.Err(); err != nil {
		return application.Plan{}, err
	}
	binding, err := r.toolsets.ResolveBinding(ctx, caller.WorkspaceID, q.ToolsetVersionID, q.ToolID, q.ToolVersion)
	if err != nil {
		if e := mapContext(err); e != nil {
			return application.Plan{}, e
		}
		if errors.Is(err, distribution.ErrNotFound) {
			return application.Plan{}, application.ErrForbidden
		}
		return application.Plan{}, application.ErrUnavailable
	}
	tool, err := r.catalog.ResolveToolVersion(ctx, binding.ToolVersionID, q.ToolID, q.ToolVersion)
	if err != nil {
		if e := mapContext(err); e != nil {
			return application.Plan{}, e
		}
		if errors.Is(err, catalog.ErrNotFound) {
			return application.Plan{}, application.ErrForbidden
		}
		return application.Plan{}, application.ErrUnavailable
	}
	now := r.clock.Now().UTC().Truncate(time.Microsecond)
	if now.IsZero() {
		return application.Plan{}, application.ErrUnavailable
	}
	access, err := r.connections.ResolveAccess(ctx, caller.WorkspaceID, caller.SubjectID, q.ConnectionID, tool.ProviderID, now)
	if err != nil {
		if e := mapContext(err); e != nil {
			return application.Plan{}, e
		}
		if errors.Is(err, connections.ErrForbidden) {
			return application.Plan{}, application.ErrForbidden
		}
		return application.Plan{}, application.ErrUnavailable
	}
	terms, err := r.pricing.ResolveAdmissionTerms(ctx, caller.WorkspaceID, binding.BudgetID, tool.PriceVersionID, tool.ID, q.Currency, now)
	if err != nil {
		if e := mapContext(err); e != nil {
			return application.Plan{}, e
		}
		if errors.Is(err, commerce.ErrPriceUnavailable) {
			return application.Plan{}, application.ErrForbidden
		}
		if errors.Is(err, commerce.ErrPricingBudget) {
			return application.Plan{}, application.ErrBudgetUnavailable
		}
		return application.Plan{}, application.ErrUnavailable
	}
	validUntil := terms.ValidUntil
	if access.ValidUntil.Before(validUntil) {
		validUntil = access.ValidUntil
	}
	return application.Plan{ToolID: q.ToolID, ToolVersion: q.ToolVersion, ToolVersionID: tool.ID, ToolsetVersionID: binding.ToolsetVersionID, ConnectionID: access.ConnectionID, PriceVersionID: terms.PriceVersionID, DeploymentRevision: tool.DeploymentRevision, BudgetID: terms.BudgetID, PeriodID: terms.PeriodID, Currency: terms.Currency, ReserveMicro: terms.ReserveMicro, ValidUntil: validUntil}, nil
}

var _ application.Resolver = (*Resolver)(nil)
