package capabilities

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
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

// validateArguments keeps JSON Schema interpretation in the outbound adapter.
// Admission/application only sees canonical business arguments and the resolved
// immutable Plan; SDK/schema implementation details never cross the port.
//
// Legacy ToolVersions may not yet publish an input schema. Preserve that
// compatibility boundary until those contracts are migrated, but whenever a
// schema is present every StartRun entrypoint is validated here before
// Connection or pricing capabilities are consulted and before quota is held.
func validateArguments(schemaJSON, canonicalArguments string) error {
	if schemaJSON == "" {
		return nil
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		return application.ErrUnavailable
	}
	// No Loader is supplied: remote/nested external references fail closed and
	// can never trigger network access during admission.
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return application.ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(canonicalArguments)))
	decoder.UseNumber()
	var instance any
	if err = decoder.Decode(&instance); err != nil {
		return application.ErrInvalid
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return application.ErrInvalid
	}
	instance, err = normalizeNumbers(instance)
	if err != nil {
		return err
	}
	if err = resolved.Validate(instance); err != nil {
		return application.ErrInvalid
	}
	return nil
}

// jsonschema-go treats json.Number's underlying string kind as a string for
// JSON type matching even though it uses json.Number for numeric equality.
// Convert integer lexemes to exact Go integer types before validation while
// leaving the canonical argument bytes untouched for idempotency and dispatch.
// Non-integer JSON numbers follow encoding/json's float64 representability.
func normalizeNumbers(value any) (any, error) {
	switch value := value.(type) {
	case json.Number:
		raw := value.String()
		if !strings.ContainsAny(raw, ".eE") {
			if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
				return n, nil
			}
			if !strings.HasPrefix(raw, "-") {
				if n, err := strconv.ParseUint(raw, 10, 64); err == nil {
					return n, nil
				}
			}
		}
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, application.ErrInvalid
		}
		return n, nil
	case map[string]any:
		for key, child := range value {
			normalized, err := normalizeNumbers(child)
			if err != nil {
				return nil, err
			}
			value[key] = normalized
		}
		return value, nil
	case []any:
		for i, child := range value {
			normalized, err := normalizeNumbers(child)
			if err != nil {
				return nil, err
			}
			value[i] = normalized
		}
		return value, nil
	default:
		return value, nil
	}
}

func (r *Resolver) Resolve(ctx context.Context, caller application.Caller, q application.Request, canonicalArguments string) (application.Plan, error) {
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
	if caller.Start != nil && binding.ToolVersionID != caller.Start.ToolVersionID {
		return application.Plan{}, application.ErrForbidden
	}
	// Direct Toolset publication may fix a Connection. Legacy/meta-tool bindings
	// leave ConnectionID empty and preserve the existing explicit request path.
	if binding.ConnectionID != "" && binding.ConnectionID != q.ConnectionID {
		return application.Plan{}, application.ErrForbidden
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
	if err = validateArguments(tool.InputSchema, canonicalArguments); err != nil {
		return application.Plan{}, err
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
