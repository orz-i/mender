package bootstrap

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	cf "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/inbound/reservations"
	cp "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/outbound/postgres"
	ca "github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	ef "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/admission"
	ep "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	ea "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/capabilities"
	uow "github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/postgres"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
	"github.com/orz-i/mender/backend/migrations"
)

func BuildCancellation(ctx context.Context, pool *pgxpool.Pool, auth application.CancelAuthorizer) (*application.Cancellation, error) {
	if pool == nil || auth == nil {
		return nil, application.ErrUnavailable
	}
	if e := migrations.Verify(ctx, pool); e != nil {
		return nil, e
	}
	if e := postgres.CancellationRole(ctx, pool); e != nil {
		return nil, e
	}
	factory := func(tx pgx.Tx, w string) (application.CancelScope, error) {
		c, e := ca.NewReleaseService(cp.NewReleases(tx))
		if e != nil {
			return nil, e
		}
		r, e := ea.NewCancellationService(ep.NewCancellations(tx))
		if e != nil {
			return nil, e
		}
		return capabilities.NewCancelScope(w, cf.NewReleases(c), ef.NewCancellations(r)), nil
	}
	return application.NewCancellation(auth, systemClock{}, uow.NewCancelUnitOfWork(pool, factory))
}
