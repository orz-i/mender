package fixedtools

import (
	"context"
	"errors"
	"time"

	catalog "github.com/orz-i/mender/backend/internal/contexts/catalog/public"
	connections "github.com/orz-i/mender/backend/internal/contexts/connections/public"
	distribution "github.com/orz-i/mender/backend/internal/contexts/distribution/public"
	"github.com/orz-i/mender/backend/internal/processes/mcpbridge/application"
)

type Clock interface{ Now() time.Time }

type Access struct {
	toolsets    distribution.Toolsets
	catalog     catalog.Catalog
	connections connections.Connections
	clock       Clock
}

func New(toolsets distribution.Toolsets, catalogPort catalog.Catalog, connectionsPort connections.Connections, clock Clock) (*Access, error) {
	if toolsets == nil || catalogPort == nil || connectionsPort == nil || clock == nil {
		return nil, application.ErrUnavailable
	}
	return &Access{toolsets: toolsets, catalog: catalogPort, connections: connectionsPort, clock: clock}, nil
}

func mapFailure(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return application.ErrUnavailable
}

func (a *Access) List(ctx context.Context, caller application.Caller, toolsetID string) ([]application.DirectTool, error) {
	bindings, err := a.toolsets.ListDirectBindings(ctx, caller.WorkspaceID, toolsetID)
	if err != nil {
		return nil, mapFailure(err)
	}
	now := a.clock.Now().UTC().Truncate(time.Microsecond)
	if now.IsZero() {
		return nil, application.ErrUnavailable
	}
	result := make([]application.DirectTool, 0, len(bindings))
	for _, binding := range bindings {
		tool, e := a.catalog.ResolveToolVersion(ctx, binding.ToolVersionID, binding.ToolID, binding.ToolVersion)
		if e != nil {
			if errors.Is(e, catalog.ErrNotFound) {
				continue
			}
			return nil, mapFailure(e)
		}
		if !tool.MCPPublishable {
			continue
		}
		access, e := a.connections.ResolveAccess(ctx, caller.WorkspaceID, caller.SubjectID, binding.ConnectionID, tool.ProviderID, now)
		if e != nil {
			if errors.Is(e, connections.ErrForbidden) {
				continue
			}
			return nil, mapFailure(e)
		}
		if access.ConnectionID != binding.ConnectionID || access.ProviderID != tool.ProviderID {
			return nil, application.ErrUnavailable
		}
		result = append(result, application.DirectTool{Name: binding.MCPName, Title: tool.Title, Description: tool.Description, ToolID: binding.ToolID, ToolVersion: binding.ToolVersion, ToolVersionID: binding.ToolVersionID, ConnectionID: binding.ConnectionID, InputSchema: tool.InputSchema, OutputSchema: tool.OutputSchema, SideEffect: tool.SideEffect, Idempotency: tool.Idempotency})
	}
	return result, nil
}

func (a *Access) Resolve(ctx context.Context, caller application.Caller, toolsetID, name string) (application.DirectTool, error) {
	items, err := a.List(ctx, caller, toolsetID)
	if err != nil {
		return application.DirectTool{}, err
	}
	for _, item := range items {
		if item.Name == name {
			return item, nil
		}
	}
	return application.DirectTool{}, application.ErrNotFound
}

var _ application.DirectTools = (*Access)(nil)
