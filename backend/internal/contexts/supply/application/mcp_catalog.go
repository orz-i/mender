package application

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
)

var ErrMCPToolCatalogUnavailable = errors.New("MCP tool catalog unavailable")

type MCPToolSnapshotRepository interface {
	AppendMCPToolSnapshot(context.Context, domain.MCPToolSnapshot) error
	ListLatestMCPToolSnapshots(context.Context, string) ([]domain.MCPToolSnapshot, error)
}

type MCPToolCatalog struct{ repository MCPToolSnapshotRepository }

func NewMCPToolCatalog(repository MCPToolSnapshotRepository) (*MCPToolCatalog, error) {
	if repository == nil {
		return nil, ErrMCPToolCatalogUnavailable
	}
	return &MCPToolCatalog{repository: repository}, nil
}

func (s *MCPToolCatalog) Record(ctx context.Context, snapshot domain.MCPToolSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if snapshot.Validate() != nil {
		return ErrMCPToolCatalogUnavailable
	}
	return s.repository.AppendMCPToolSnapshot(ctx, snapshot)
}

func (s *MCPToolCatalog) ListLatest(ctx context.Context, deploymentRevision string) ([]domain.MCPToolSnapshot, error) {
	if ctx.Err() != nil || !validDeploymentID(deploymentRevision) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, ErrMCPToolCatalogUnavailable
	}
	items, err := s.repository.ListLatestMCPToolSnapshots(ctx, deploymentRevision)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.Validate() != nil || item.DeploymentRevision != deploymentRevision {
			return nil, ErrMCPToolCatalogUnavailable
		}
	}
	return items, nil
}

func validDeploymentID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
