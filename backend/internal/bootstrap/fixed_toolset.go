package bootstrap

import (
	"github.com/jackc/pgx/v5/pgxpool"
	catalogfacade "github.com/orz-i/mender/backend/internal/contexts/catalog/adapters/inbound/facade"
	catalogpg "github.com/orz-i/mender/backend/internal/contexts/catalog/adapters/outbound/postgres"
	catalogapp "github.com/orz-i/mender/backend/internal/contexts/catalog/application"
	connectionsfacade "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/inbound/facade"
	connectionspg "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/postgres"
	connectionsapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
	distributionfacade "github.com/orz-i/mender/backend/internal/contexts/distribution/adapters/inbound/facade"
	distributionpg "github.com/orz-i/mender/backend/internal/contexts/distribution/adapters/outbound/postgres"
	distributionapp "github.com/orz-i/mender/backend/internal/contexts/distribution/application"
	"github.com/orz-i/mender/backend/internal/processes/mcpbridge/adapters/outbound/fixedtools"
	mcpapp "github.com/orz-i/mender/backend/internal/processes/mcpbridge/application"
)

// BuildFixedToolRegistry composes only read-only bounded-context capabilities.
// Admission remains the sole authority for creating Runs and reserving quota.
func BuildFixedToolRegistry(pool *pgxpool.Pool) (mcpapp.DirectTools, error) {
	if pool == nil {
		return nil, mcpapp.ErrUnavailable
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
	return fixedtools.New(
		distributionfacade.New(distributionService),
		catalogfacade.New(catalogService),
		connectionsfacade.New(connectionsService),
		systemClock{},
	)
}
