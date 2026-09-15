package bootstrap

import (
	"context"
	"errors"
	"sync"

	connfacade "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/inbound/facade"
	connpg "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/postgres"
	connapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
	execfacade "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/facade"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/agentinput"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/supplyinput"
	execapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	connectioncredentials "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/connectioncredentials"
	executioninput "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/executioninput"
	httpexecutor "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/http"
	supplypg "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/postgres"
	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
	supplydomain "github.com/orz-i/mender/backend/internal/contexts/supply/domain"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

type AgentInputRuntimeConfig struct {
	DatabaseURL         string
	ExecutorDatabaseURL string
	ProviderIDs         []string
	AllowedHosts        []string
	AllowHTTP           bool
	AllowLoopback       bool
}

// BuildAgentInputRuntime keeps user-authorized input mutation separate from
// Supply deployment/credential reads. Raw answer bytes exist only in the
// request/application/transport call and never enter the execution database.
func BuildAgentInputRuntime(ctx context.Context, c AgentInputRuntimeConfig, authorizer ports.Authorizer, secrets supplyapp.SecretProvider) (*execapp.AgentInputSubmissions, func(), error) {
	if authorizer == nil || secrets == nil || c.DatabaseURL == "" || c.ExecutorDatabaseURL == "" || len(c.ProviderIDs) < 1 || len(c.ProviderIDs) > 256 {
		return nil, nil, errors.New("Agent input runtime is not safely configured")
	}
	if err := sameDatabaseDistinctRoles(c.DatabaseURL, c.ExecutorDatabaseURL); err != nil {
		return nil, nil, errors.New("Agent input runtime requires the same database with distinct restricted roles")
	}
	seen := map[string]bool{}
	for _, providerID := range c.ProviderIDs {
		probe := execapp.AgentInputSubmissionTarget{WorkspaceID: "ws_probe", RunID: "run_probe", AttemptNo: 1, ProviderID: providerID, ProviderRequestID: "request/probe", ExternalTaskID: "task/probe", InputRequestID: "input.probe", InputSchemaJSON: `{"type":"object"}`, State: "pending"}
		if !probe.Valid() || seen[providerID] {
			return nil, nil, errors.New("Agent input runtime contains invalid or duplicate provider")
		}
		seen[providerID] = true
	}
	inputPool, err := database.Open(ctx, c.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	closeInput := true
	defer func() {
		if closeInput {
			inputPool.Close()
		}
	}()
	if err = migrations.Verify(ctx, inputPool); err != nil {
		return nil, nil, err
	}
	if err = database.AgentInputSenderRole(ctx, inputPool); err != nil {
		return nil, nil, err
	}
	executorPool, err := database.Open(ctx, c.ExecutorDatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	closeExecutor := true
	defer func() {
		if closeExecutor {
			executorPool.Close()
		}
	}()
	if err = migrations.Verify(ctx, executorPool); err != nil {
		return nil, nil, err
	}
	if err = database.ExecutorRole(ctx, executorPool); err != nil {
		return nil, nil, err
	}
	inputService, err := execapp.NewRuntimeInputService(postgres.NewRuntimeInputs(executorPool))
	if err != nil {
		return nil, nil, err
	}
	credentialService, err := connapp.NewRuntimeService(connpg.NewRuntimeRepository(executorPool))
	if err != nil {
		return nil, nil, err
	}
	broker, err := supplyapp.NewBroker(
		executioninput.New(execfacade.NewRuntimeInputs(inputService)),
		connectioncredentials.New(connfacade.NewRuntimeCredentials(credentialService)),
		supplypg.NewDeployments(executorPool), secrets,
	)
	if err != nil {
		return nil, nil, err
	}
	httpInput, err := httpexecutor.NewForTransports(broker, httpexecutor.EgressPolicy{AllowedHosts: c.AllowedHosts, AllowHTTP: c.AllowHTTP, AllowLoopback: c.AllowLoopback}, []string{supplydomain.TransportAgentHTTP}, nil, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	senders := make(map[string]supply.ProviderInputSender, len(c.ProviderIDs))
	for _, providerID := range c.ProviderIDs {
		senders[providerID] = httpInput
	}
	source, err := supplyinput.New(senders)
	if err != nil {
		return nil, nil, err
	}
	service, err := execapp.NewAgentInputSubmissions(authorizer, postgres.NewAgentInputs(inputPool), agentinput.Preparer{}, source, systemClock{})
	if err != nil {
		return nil, nil, err
	}
	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			inputPool.Close()
			executorPool.Close()
		})
	}
	closeInput, closeExecutor = false, false
	return service, closeAll, nil
}
