package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/catalog/application"
	"github.com/orz-i/mender/backend/internal/contexts/catalog/domain"
)

type Repository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) FindToolVersion(ctx context.Context, id string) (domain.ToolVersion, error) {
	var v domain.ToolVersion
	err := r.pool.QueryRow(ctx, `SELECT id,tool_id,version,provider_id,price_version_id,deployment_revision,state,published_at FROM catalog.tool_versions WHERE id=$1`, id).Scan(&v.ID, &v.ToolID, &v.Version, &v.ProviderID, &v.PriceVersionID, &v.DeploymentRevision, &v.State, &v.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ToolVersion{}, application.ErrNotFound
	}
	if err != nil {
		return domain.ToolVersion{}, application.ErrUnavailable
	}
	return v, nil
}

var _ application.Repository = (*Repository)(nil)
