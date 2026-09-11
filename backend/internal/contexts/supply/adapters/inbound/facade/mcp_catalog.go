package facade

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type MCPCatalog struct{ service *application.MCPToolCatalog }

func NewMCPCatalog(service *application.MCPToolCatalog) *MCPCatalog {
	return &MCPCatalog{service: service}
}

func (f *MCPCatalog) ListMCPToolCandidates(ctx context.Context, revision string) ([]supply.MCPToolCandidate, error) {
	if f == nil || f.service == nil {
		return nil, supply.ErrMCPDiscoveryUnavailable
	}
	items, err := f.service.ListLatest(ctx, revision)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, supply.ErrMCPDiscoveryUnavailable
	}
	result := make([]supply.MCPToolCandidate, 0, len(items))
	for _, item := range items {
		result = append(result, supply.MCPToolCandidate{DeploymentRevision: item.DeploymentRevision, ToolName: item.ToolName, Title: item.Title, Description: item.Description, InputSchema: item.InputSchema, OutputSchema: item.OutputSchema, AnnotationsJSON: item.AnnotationsJSON, ContentSHA256: item.ContentSHA256, DiscoveredAt: item.DiscoveredAt})
	}
	return result, nil
}

var _ supply.MCPDiscoveryCatalog = (*MCPCatalog)(nil)
