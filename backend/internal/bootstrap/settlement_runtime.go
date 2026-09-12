package bootstrap

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	commercesettlement "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/inbound/reservations"
	commercepg "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/outbound/postgres"
	commerceapp "github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	settlementcap "github.com/orz-i/mender/backend/internal/processes/settlement/adapters/outbound/capabilities"
	settlementpg "github.com/orz-i/mender/backend/internal/processes/settlement/adapters/outbound/postgres"
	settlementapp "github.com/orz-i/mender/backend/internal/processes/settlement/application"
	"github.com/orz-i/mender/backend/migrations"
)

type UsageSettlementRuntimeConfig struct{ DatabaseURL string }

type usageSettlementRunner interface {
	SettleOne(context.Context, string) (settlementapp.Receipt, error)
}

// ReviewedUsageSettlementRuntime is a bounded library composition. It has no
// scheduler and performs no network calls; a reviewed host chooses workspaces
// and cadence later.
type ReviewedUsageSettlementRuntime struct{ service usageSettlementRunner }

func (r *ReviewedUsageSettlementRuntime) SettleOne(ctx context.Context, workspace string) (settlementapp.Receipt, error) {
	if r == nil || r.service == nil {
		return settlementapp.Receipt{}, errors.New("usage settlement runtime is not safely configured")
	}
	return r.service.SettleOne(ctx, workspace)
}

func BuildUsageSettlementRuntime(ctx context.Context, c UsageSettlementRuntimeConfig) (*ReviewedUsageSettlementRuntime, func(), error) {
	if c.DatabaseURL == "" {
		return nil, nil, errors.New("usage settlement runtime is not safely configured")
	}
	pool, err := database.Open(ctx, c.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*ReviewedUsageSettlementRuntime, func(), error) {
		pool.Close()
		return nil, nil, err
	}
	if err = migrations.Verify(ctx, pool); err != nil {
		return fail(err)
	}
	if err = database.SettlementRole(ctx, pool); err != nil {
		return fail(err)
	}
	uow := settlementpg.New(pool, func(tx pgx.Tx, workspace string) (settlementapp.Scope, error) {
		commerceService, e := commerceapp.NewSettlementService(commercepg.NewSettlements(tx))
		if e != nil {
			return nil, e
		}
		return settlementcap.New(settlementpg.NewJobs(tx, workspace), commercesettlement.NewSettlement(commerceService))
	})
	service, err := settlementapp.New(uow, systemClock{})
	if err != nil {
		return fail(err)
	}
	return &ReviewedUsageSettlementRuntime{service: service}, pool.Close, nil
}
