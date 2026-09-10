package bootstrap

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	cfacade "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/inbound/reservations"
	cpg "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/outbound/postgres"
	capp "github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	efacade "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/admission"
	epg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	eapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/capabilities"
	"github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/input"
	upg "github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/postgres"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
	"github.com/orz-i/mender/backend/migrations"
)

// BuildAdmission does not register HTTP routes. Real authorization and catalog/price
// resolution must be explicitly supplied; this slice installs neither into the running API.
func BuildAdmission(ctx context.Context, pool *pgxpool.Pool, auth application.Authorizer, resolver application.Resolver) (*application.Service, error) {
	if pool == nil || auth == nil || resolver == nil {
		return nil, application.ErrUnavailable
	}
	if e := migrations.Verify(ctx, pool); e != nil {
		return nil, e
	}
	if e := database.AdmissionRole(ctx, pool); e != nil {
		return nil, e
	}
	factory := func(tx pgx.Tx, w string) (application.Scope, error) {
		c, e := capp.NewReservationService(cpg.NewReservations(tx))
		if e != nil {
			return nil, e
		}
		r, e := eapp.NewAdmissionService(epg.NewAdmissions(tx))
		if e != nil {
			return nil, e
		}
		return capabilities.New(w, cfacade.New(c), efacade.New(r)), nil
	}
	return application.New(auth, resolver, input.Codec{}, input.IDs{}, systemClock{}, upg.New(pool, factory))
}
