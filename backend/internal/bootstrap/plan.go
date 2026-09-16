package bootstrap

import (
	"github.com/jackc/pgx/v5/pgxpool"
	catalogfacade "github.com/orz-i/mender/backend/internal/contexts/catalog/adapters/inbound/facade"
	catalogpg "github.com/orz-i/mender/backend/internal/contexts/catalog/adapters/outbound/postgres"
	catalogapp "github.com/orz-i/mender/backend/internal/contexts/catalog/application"
	pricingfacade "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/inbound/pricing"
	commercepg "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/outbound/postgres"
	commerceapp "github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	connectionsfacade "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/inbound/facade"
	connectionspg "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/postgres"
	connectionsapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
	distributionfacade "github.com/orz-i/mender/backend/internal/contexts/distribution/adapters/inbound/facade"
	distributionpg "github.com/orz-i/mender/backend/internal/contexts/distribution/adapters/outbound/postgres"
	distributionapp "github.com/orz-i/mender/backend/internal/contexts/distribution/application"
	supplyfacade "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/inbound/facade"
	supplypg "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/postgres"
	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
	"github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/capabilities"
	admissionapp "github.com/orz-i/mender/backend/internal/processes/admission/application"
)

// BuildAdmissionResolver composes read-only public capabilities from their owning
// bounded contexts. The admission writer is deliberately not accepted here.
func BuildAdmissionResolver(pool *pgxpool.Pool) (admissionapp.Resolver, error) {
	if pool == nil {
		return nil, admissionapp.ErrUnavailable
	}
	catalogService, err := catalogapp.New(catalogpg.New(pool))
	if err != nil {
		return nil, err
	}
	distributionService, err := distributionapp.New(distributionpg.New(pool))
	if err != nil {
		return nil, err
	}
	connectionsService, err := connectionsapp.New(connectionspg.New(pool))
	if err != nil {
		return nil, err
	}
	pricingService, err := commerceapp.NewPricingService(commercepg.NewPricing(pool))
	if err != nil {
		return nil, err
	}
	releaseRouting, err := supplyapp.NewReleaseRouting(supplypg.NewReleaseRoutingRepository(pool))
	if err != nil {
		return nil, err
	}
	providerAdmission, err := supplyapp.NewProviderAdmission(supplypg.NewDeployments(pool))
	if err != nil {
		return nil, err
	}
	return capabilities.NewResolver(
		distributionfacade.New(distributionService),
		catalogfacade.New(catalogService),
		connectionsfacade.New(connectionsService),
		pricingfacade.New(pricingService),
		supplyfacade.NewProviderAdmissionGate(providerAdmission),
		supplyfacade.NewReleaseRouter(releaseRouting),
		systemClock{},
	)
}
