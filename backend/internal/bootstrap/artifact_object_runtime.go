package bootstrap

import (
	"context"
	"errors"
	"time"

	objectfs "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/objectfs"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	rundomain "github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

type ArtifactObjectRuntimeConfig struct {
	DatabaseURL string
	Root        string
	Retention   time.Duration
}

type ReviewedArtifactObjectRuntime struct {
	materializer *runapp.ArtifactObjectMaterializer
}

type ArtifactObjectCycleResult struct {
	Materialized bool
	Expired      bool
}

func BuildArtifactObjectRuntime(ctx context.Context, c ArtifactObjectRuntimeConfig) (*ReviewedArtifactObjectRuntime, func(), error) {
	if c.DatabaseURL == "" || c.Root == "" || c.Retention < time.Hour || c.Retention > 90*24*time.Hour {
		return nil, nil, errors.New("artifact object runtime is not safely configured")
	}
	pool, err := database.Open(ctx, c.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*ReviewedArtifactObjectRuntime, func(), error) {
		pool.Close()
		return nil, nil, err
	}
	if err = migrations.Verify(ctx, pool); err != nil {
		return fail(err)
	}
	if err = database.ArtifactMaterializerRole(ctx, pool); err != nil {
		return fail(err)
	}
	store, err := objectfs.New(c.Root)
	if err != nil {
		return fail(err)
	}
	materializer, err := runapp.NewArtifactObjectMaterializer(runpg.New(pool), store, systemClock{}, c.Retention)
	if err != nil {
		return fail(err)
	}
	return &ReviewedArtifactObjectRuntime{materializer: materializer}, pool.Close, nil
}

func (r *ReviewedArtifactObjectRuntime) CycleOne(ctx context.Context, workspace rundomain.WorkspaceID) (ArtifactObjectCycleResult, error) {
	var result ArtifactObjectCycleResult
	if r == nil || r.materializer == nil || !workspace.IsValid() {
		return result, errors.New("artifact object runtime is not safely configured")
	}
	_, err := r.materializer.MaterializeOne(ctx, workspace)
	switch {
	case err == nil:
		result.Materialized = true
	case errors.Is(err, runapp.ErrNoArtifactObjectCandidate):
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return result, err
	default:
		return result, err
	}
	_, err = r.materializer.ExpireOne(ctx, workspace)
	switch {
	case err == nil:
		result.Expired = true
	case errors.Is(err, runapp.ErrNoExpiredArtifactObject):
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return result, err
	default:
		return result, err
	}
	return result, nil
}
