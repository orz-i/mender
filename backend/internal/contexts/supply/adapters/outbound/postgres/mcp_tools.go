package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
)

type MCPToolSnapshots struct{ pool *pgxpool.Pool }

func NewMCPToolSnapshots(pool *pgxpool.Pool) *MCPToolSnapshots { return &MCPToolSnapshots{pool: pool} }

func (r *MCPToolSnapshots) AppendMCPToolSnapshot(ctx context.Context, s domain.MCPToolSnapshot) error {
	if r == nil || r.pool == nil || s.Validate() != nil {
		return application.ErrMCPToolCatalogUnavailable
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO supply.mcp_tool_snapshots(deployment_revision,tool_name,title,description,input_schema,output_schema,annotations,content_sha256,discovered_at) VALUES($1,$2,$3,$4,$5::jsonb,NULLIF($6,'')::jsonb,$7::jsonb,$8,$9) ON CONFLICT DO NOTHING`, s.DeploymentRevision, s.ToolName, s.Title, s.Description, s.InputSchema, s.OutputSchema, s.AnnotationsJSON, s.ContentSHA256, s.DiscoveredAt)
	if err != nil {
		return application.ErrMCPToolCatalogUnavailable
	}
	return nil
}

func (r *MCPToolSnapshots) ListLatestMCPToolSnapshots(ctx context.Context, revision string) ([]domain.MCPToolSnapshot, error) {
	if r == nil || r.pool == nil {
		return nil, application.ErrMCPToolCatalogUnavailable
	}
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT ON (tool_name) deployment_revision,tool_name,title,description,input_schema::text,COALESCE(output_schema::text,''),annotations::text,content_sha256,discovered_at FROM supply.mcp_tool_snapshots WHERE deployment_revision=$1 ORDER BY tool_name,discovered_at DESC,content_sha256 DESC`, revision)
	if err != nil {
		return nil, application.ErrMCPToolCatalogUnavailable
	}
	defer rows.Close()
	var result []domain.MCPToolSnapshot
	for rows.Next() {
		var s domain.MCPToolSnapshot
		if err = rows.Scan(&s.DeploymentRevision, &s.ToolName, &s.Title, &s.Description, &s.InputSchema, &s.OutputSchema, &s.AnnotationsJSON, &s.ContentSHA256, &s.DiscoveredAt); err != nil || s.Validate() != nil {
			return nil, application.ErrMCPToolCatalogUnavailable
		}
		result = append(result, s)
	}
	if err = rows.Err(); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, application.ErrMCPToolCatalogUnavailable
	}
	return result, nil
}

var _ application.MCPToolSnapshotRepository = (*MCPToolSnapshots)(nil)
