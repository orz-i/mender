package bootstrap

import (
	"context"
	"errors"

	connfacade "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/inbound/facade"
	connpg "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/postgres"
	connapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
	execfacade "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/facade"
	execpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/supplyexecutor"
	execapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	connectioncredentials "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/connectioncredentials"
	executioninput "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/executioninput"
	httpexecutor "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/http"
	supplypg "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/postgres"
	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

type SupplierHTTPRuntimeConfig struct {
	WorkerDatabaseURL   string
	ExecutorDatabaseURL string
	AllowedHosts        []string
	AllowHTTP           bool
	AllowLoopback       bool
}

// BuildSupplierHTTPExecutor assembles the read-only execution-material plane
// separately from the Worker lease-control connection. SecretProvider remains
// an explicit injected capability; bootstrap never reads secrets from process
// environment or from execution tables.
func BuildSupplierHTTPExecutor(ctx context.Context, c SupplierHTTPRuntimeConfig, secrets supplyapp.SecretProvider) (execapp.Executor, func(), error) {
	if secrets == nil || c.WorkerDatabaseURL == "" || c.ExecutorDatabaseURL == "" {
		return nil, nil, errors.New("supplier HTTP runtime is not safely configured")
	}
	workerConfig, err := database.Config(c.WorkerDatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	executorConfig, err := database.Config(c.ExecutorDatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	workerDB, executorDB := workerConfig.ConnConfig, executorConfig.ConnConfig
	if workerDB.Host != executorDB.Host || workerDB.Port != executorDB.Port || workerDB.Database != executorDB.Database || workerDB.User == executorDB.User {
		return nil, nil, errors.New("executor runtime requires the same database with a distinct restricted role")
	}
	pool, err := database.Open(ctx, c.ExecutorDatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (execapp.Executor, func(), error) {
		pool.Close()
		return nil, nil, err
	}
	if err = migrations.Verify(ctx, pool); err != nil {
		return fail(err)
	}
	if err = database.ExecutorRole(ctx, pool); err != nil {
		return fail(err)
	}
	inputService, err := execapp.NewRuntimeInputService(execpg.NewRuntimeInputs(pool))
	if err != nil {
		return fail(err)
	}
	credentialService, err := connapp.NewRuntimeService(connpg.NewRuntimeRepository(pool))
	if err != nil {
		return fail(err)
	}
	broker, err := supplyapp.NewBroker(
		executioninput.New(execfacade.NewRuntimeInputs(inputService)),
		connectioncredentials.New(connfacade.NewRuntimeCredentials(credentialService)),
		supplypg.NewDeployments(pool),
		secrets,
	)
	if err != nil {
		return fail(err)
	}
	supplier, err := httpexecutor.New(broker, httpexecutor.EgressPolicy{
		AllowedHosts:  c.AllowedHosts,
		AllowHTTP:     c.AllowHTTP,
		AllowLoopback: c.AllowLoopback,
	}, nil, nil, nil)
	if err != nil {
		return fail(err)
	}
	executor, err := supplyexecutor.New(supplier)
	if err != nil {
		return fail(err)
	}
	return executor, pool.Close, nil
}
