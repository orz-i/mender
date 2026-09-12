package bootstrap

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	connectionhttp "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/inbound/httpapi"
	connectionidentityaccess "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/identityaccess"
	connectionpg "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/postgres"
	connectionapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
	runfacade "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/facade"
	runhttp "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/httpapi"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/coordinatedcancel"
	runcursor "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/cursor"
	runidentityaccess "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/identityaccess"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/inbound/facade"
	identityhttp "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/inbound/httpapi"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	identityoidc "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/oidc"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/sessioncodec"
	identityapp "github.com/orz-i/mender/backend/internal/contexts/identity/application"
	"github.com/orz-i/mender/backend/internal/platform/httpserver"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	cancelfacade "github.com/orz-i/mender/backend/internal/processes/admission/adapters/inbound/cancellation"
	admissionfacade "github.com/orz-i/mender/backend/internal/processes/admission/adapters/inbound/facade"
	admissionhttp "github.com/orz-i/mender/backend/internal/processes/admission/adapters/inbound/httpapi"
	admissionidentityaccess "github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/identityaccess"
	"github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/identitycancel"
	admissionapp "github.com/orz-i/mender/backend/internal/processes/admission/application"
	mcphttp "github.com/orz-i/mender/backend/internal/processes/mcpbridge/adapters/inbound/httpapi"
	mcpadmission "github.com/orz-i/mender/backend/internal/processes/mcpbridge/adapters/outbound/admissionaccess"
	mcpexecution "github.com/orz-i/mender/backend/internal/processes/mcpbridge/adapters/outbound/executionaccess"
	mcpidentity "github.com/orz-i/mender/backend/internal/processes/mcpbridge/adapters/outbound/identityaccess"
	mcpapp "github.com/orz-i/mender/backend/internal/processes/mcpbridge/application"
	"github.com/orz-i/mender/backend/migrations"
)

type APIConfig struct {
	RunAPIEnabled                bool
	RunReadAPIEnabled            bool
	DatabaseURL                  string
	CursorSigningKey             []byte
	CoordinatedCancelEnabled     bool
	ProviderCancelEnabled        bool
	CancellationDatabaseURL      string
	StartRunAPIEnabled           bool
	AdmissionDatabaseURL         string
	MCPGatewayEnabled            bool
	MCPFixedToolsetEnabled       bool
	ConsoleOIDCEnabled           bool
	BrowserSessionDatabaseURL    string
	OIDCIssuer                   string
	OIDCClientID                 string
	OIDCClientSecretFile         string
	OIDCRedirectURL              string
	FlowSigningKeyFile           string
	ConsoleCookieSecure          bool
	ConsoleSessionTTL            time.Duration
	ConsoleRunDelegationEnabled  bool
	ConsoleRunDelegationTTL      time.Duration
	ConsoleConnectionsEnabled    bool
	ConnectionManagerDatabaseURL string
}

func loadConsoleSecrets(clientSecretFile, signingKeyFile string) (string, []byte, error) {
	clientSecretRaw, err := os.ReadFile(clientSecretFile)
	if err != nil {
		return "", nil, errors.New("OIDC client secret unavailable")
	}
	clientSecret := strings.TrimSpace(string(clientSecretRaw))
	if clientSecret == "" || len(clientSecret) > 4096 {
		return "", nil, errors.New("OIDC client secret unavailable")
	}
	keyRaw, err := os.ReadFile(signingKeyFile)
	if err != nil {
		return "", nil, errors.New("OIDC flow signing key unavailable")
	}
	encoded := strings.TrimSpace(string(keyRaw))
	key, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(encoded) != 43 || len(key) != 32 || base64.RawURLEncoding.EncodeToString(key) != encoded {
		return "", nil, errors.New("OIDC flow signing key must be 32-byte unpadded base64url")
	}
	return clientSecret, key, nil
}

func LoadAPIConfig(getenv func(string) string) (APIConfig, error) {
	c := APIConfig{ConsoleCookieSecure: true, ConsoleSessionTTL: 8 * time.Hour, ConsoleRunDelegationTTL: 10 * time.Minute}
	switch getenv("MENDER_CONSOLE_OIDC_ENABLED") {
	case "", "false":
	case "true":
		c.ConsoleOIDCEnabled = true
		c.BrowserSessionDatabaseURL = getenv("MENDER_BROWSER_SESSION_DATABASE_URL")
		c.OIDCIssuer = strings.TrimSpace(getenv("MENDER_CONSOLE_OIDC_ISSUER"))
		c.OIDCClientID = strings.TrimSpace(getenv("MENDER_CONSOLE_OIDC_CLIENT_ID"))
		c.OIDCClientSecretFile = strings.TrimSpace(getenv("MENDER_CONSOLE_OIDC_CLIENT_SECRET_FILE"))
		c.OIDCRedirectURL = strings.TrimSpace(getenv("MENDER_CONSOLE_OIDC_REDIRECT_URL"))
		c.FlowSigningKeyFile = strings.TrimSpace(getenv("MENDER_CONSOLE_FLOW_SIGNING_KEY_FILE"))
		if c.BrowserSessionDatabaseURL == "" || c.OIDCIssuer == "" || c.OIDCClientID == "" || c.OIDCClientSecretFile == "" || c.OIDCRedirectURL == "" || c.FlowSigningKeyFile == "" {
			return APIConfig{}, errors.New("Console OIDC requires session database, issuer, client, redirect and mounted secret files")
		}
		switch getenv("MENDER_CONSOLE_COOKIE_SECURE") {
		case "", "true":
		case "false":
			c.ConsoleCookieSecure = false
			if !(strings.HasPrefix(c.OIDCRedirectURL, "http://127.0.0.1:") || strings.HasPrefix(c.OIDCRedirectURL, "http://localhost:")) {
				return APIConfig{}, errors.New("insecure Console cookie is only allowed for loopback OIDC redirect")
			}
		default:
			return APIConfig{}, errors.New("MENDER_CONSOLE_COOKIE_SECURE must be true or false")
		}
		if raw := getenv("MENDER_CONSOLE_SESSION_TTL"); raw != "" {
			ttl, err := time.ParseDuration(raw)
			if err != nil || ttl < 5*time.Minute || ttl > 24*time.Hour {
				return APIConfig{}, errors.New("Console session TTL must be between 5m and 24h")
			}
			c.ConsoleSessionTTL = ttl
		}
	default:
		return c, errors.New("MENDER_CONSOLE_OIDC_ENABLED must be true or false")
	}
	switch getenv("MENDER_CONSOLE_CONNECTIONS_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleOIDCEnabled {
			return APIConfig{}, errors.New("Console Connections require Console OIDC")
		}
		c.ConsoleConnectionsEnabled = true
		c.ConnectionManagerDatabaseURL = getenv("MENDER_CONNECTION_MANAGER_DATABASE_URL")
		if c.ConnectionManagerDatabaseURL == "" {
			return APIConfig{}, errors.New("Console Connections require a separate connection-manager database role")
		}
	default:
		return c, errors.New("MENDER_CONSOLE_CONNECTIONS_ENABLED must be true or false")
	}
	switch getenv("MENDER_RUN_START_API_ENABLED") {
	case "", "false":
	case "true":
		c.StartRunAPIEnabled = true
		c.AdmissionDatabaseURL = getenv("MENDER_ADMISSION_DATABASE_URL")
		if c.AdmissionDatabaseURL == "" {
			return APIConfig{}, errors.New("StartRun API requires a separate admission database role")
		}
	default:
		return c, errors.New("MENDER_RUN_START_API_ENABLED must be true or false")
	}
	switch getenv("MENDER_RUN_COORDINATED_CANCEL_ENABLED") {
	case "", "false":
	case "true":
		c.CoordinatedCancelEnabled = true
	default:
		return c, errors.New("MENDER_RUN_COORDINATED_CANCEL_ENABLED must be true or false")
	}
	switch getenv("MENDER_RUN_PROVIDER_CANCEL_ENABLED") {
	case "", "false":
	case "true":
		c.ProviderCancelEnabled = true
	default:
		return c, errors.New("MENDER_RUN_PROVIDER_CANCEL_ENABLED must be true or false")
	}
	if c.ProviderCancelEnabled && !c.CoordinatedCancelEnabled {
		return c, errors.New("provider cancellation requires coordinated cancellation")
	}
	if c.CoordinatedCancelEnabled {
		c.CancellationDatabaseURL = getenv("MENDER_CANCELLATION_DATABASE_URL")
		if c.CancellationDatabaseURL == "" {
			return APIConfig{}, errors.New("coordinated cancellation requires a separate cancellation database role")
		}
	}
	switch getenv("MENDER_RUN_READ_API_ENABLED") {
	case "", "false":
	case "true":
		c.RunReadAPIEnabled = true
	default:
		return c, errors.New("MENDER_RUN_READ_API_ENABLED must be true or false")
	}
	switch getenv("MENDER_CONSOLE_RUN_DELEGATION_ENABLED") {
	case "", "false":
	case "true":
		c.ConsoleRunDelegationEnabled = true
		if raw := getenv("MENDER_CONSOLE_RUN_DELEGATION_TTL"); raw != "" {
			ttl, err := time.ParseDuration(raw)
			if err != nil || ttl < time.Minute || ttl > 30*time.Minute {
				return APIConfig{}, errors.New("Console Run delegation TTL must be between 1m and 30m")
			}
			c.ConsoleRunDelegationTTL = ttl
		}
	default:
		return c, errors.New("MENDER_CONSOLE_RUN_DELEGATION_ENABLED must be true or false")
	}
	if c.ConsoleRunDelegationEnabled && (!c.ConsoleOIDCEnabled || !c.RunReadAPIEnabled) {
		return APIConfig{}, errors.New("Console Run delegation requires Console OIDC and Run read API")
	}
	switch getenv("MENDER_MCP_GATEWAY_ENABLED") {
	case "", "false":
	case "true":
		c.MCPGatewayEnabled = true
	default:
		return c, errors.New("MENDER_MCP_GATEWAY_ENABLED must be true or false")
	}
	if c.MCPGatewayEnabled && (!c.RunReadAPIEnabled || !c.StartRunAPIEnabled || !c.CoordinatedCancelEnabled) {
		return c, errors.New("MCP gateway requires Run read, StartRun and coordinated cancellation capabilities")
	}
	switch getenv("MENDER_MCP_FIXED_TOOLSET_ENABLED") {
	case "", "false":
	case "true":
		c.MCPFixedToolsetEnabled = true
	default:
		return c, errors.New("MENDER_MCP_FIXED_TOOLSET_ENABLED must be true or false")
	}
	if c.MCPFixedToolsetEnabled && !c.MCPGatewayEnabled {
		return c, errors.New("Fixed Toolset MCP requires the authenticated MCP gateway")
	}
	switch getenv("MENDER_RUN_API_ENABLED") {
	case "", "false":
		if c.RunReadAPIEnabled || c.CoordinatedCancelEnabled || c.ProviderCancelEnabled || c.StartRunAPIEnabled || c.MCPGatewayEnabled || c.MCPFixedToolsetEnabled || c.ConsoleOIDCEnabled || c.ConsoleRunDelegationEnabled || c.ConsoleConnectionsEnabled {
			return APIConfig{}, errors.New("Run capabilities require the authenticated Run API")
		}
		return c, nil
	case "true":
		c.RunAPIEnabled = true
	default:
		return c, errors.New("MENDER_RUN_API_ENABLED must be true or false")
	}
	c.DatabaseURL = getenv("MENDER_DATABASE_URL")
	if c.DatabaseURL == "" {
		return APIConfig{}, errors.New("MENDER_DATABASE_URL is required when Run API is enabled")
	}
	if c.RunReadAPIEnabled {
		raw := getenv("MENDER_CURSOR_SIGNING_KEY")
		key, err := base64.RawURLEncoding.Strict().DecodeString(raw)
		if err != nil || len(raw) != 43 || base64.RawURLEncoding.EncodeToString(key) != raw {
			return APIConfig{}, errors.New("Run read API requires a 32-byte base64url cursor signing key")
		}
		if _, err = runcursor.New(key); err != nil {
			return APIConfig{}, err
		}
		c.CursorSigningKey = key
	}
	return c, nil
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// BuildAPI never migrates, seeds data or falls back to a test repository.
func BuildAPI(ctx context.Context, c APIConfig) (http.Handler, func(), error) {
	if !c.RunAPIEnabled {
		if c.RunReadAPIEnabled || c.CoordinatedCancelEnabled || c.ProviderCancelEnabled || c.StartRunAPIEnabled || c.MCPGatewayEnabled || c.MCPFixedToolsetEnabled || c.ConsoleOIDCEnabled || c.ConsoleRunDelegationEnabled || c.ConsoleConnectionsEnabled {
			return nil, nil, errors.New("Run capabilities require the authenticated Run API")
		}
		return httpserver.NewRouter(), func() {}, nil
	}
	if c.MCPGatewayEnabled && (!c.RunReadAPIEnabled || !c.StartRunAPIEnabled || !c.CoordinatedCancelEnabled) {
		return nil, nil, errors.New("MCP gateway requires Run read, StartRun and coordinated cancellation capabilities")
	}
	if c.MCPFixedToolsetEnabled && !c.MCPGatewayEnabled {
		return nil, nil, errors.New("Fixed Toolset MCP requires the authenticated MCP gateway")
	}
	var codec *runcursor.Codec
	if c.RunReadAPIEnabled {
		var err error
		codec, err = runcursor.New(c.CursorSigningKey)
		if err != nil {
			return nil, nil, err
		}
	}
	start, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, err := database.Open(start, c.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	var cancelPool *pgxpool.Pool
	var admissionPool *pgxpool.Pool
	var browserSessionPool *pgxpool.Pool
	var connectionManagerPool *pgxpool.Pool
	closePools := func() {
		if connectionManagerPool != nil {
			connectionManagerPool.Close()
		}
		if browserSessionPool != nil {
			browserSessionPool.Close()
		}
		if admissionPool != nil {
			admissionPool.Close()
		}
		if cancelPool != nil {
			cancelPool.Close()
		}
		pool.Close()
	}
	failed := func(err error) (http.Handler, func(), error) { closePools(); return nil, nil, err }
	if err = migrations.Verify(start, pool); err != nil {
		return failed(err)
	}
	if err = database.RuntimeRole(start, pool); err != nil {
		return failed(err)
	}
	identity, err := identityapp.NewService(identitypg.New(pool), keycodec.Codec{}, systemClock{})
	if err != nil {
		return failed(err)
	}
	identityFacade := facade.New(identity)
	access := runidentityaccess.New(identityFacade)
	repository := runpg.New(pool)
	runs, err := runapp.NewService(repository, access, systemClock{})
	if err != nil {
		return failed(err)
	}
	if c.CoordinatedCancelEnabled {
		cancelPool, err = database.Open(start, c.CancellationDatabaseURL)
		if err != nil {
			return failed(err)
		}
		// Both connections must address the same database; never coordinate quota
		// against another instance that happens to contain matching Run identifiers.
		readerCfg, writerCfg := pool.Config().ConnConfig, cancelPool.Config().ConnConfig
		if readerCfg.Host != writerCfg.Host || readerCfg.Port != writerCfg.Port || readerCfg.Database != writerCfg.Database || readerCfg.User == writerCfg.User {
			return failed(errors.New("cancellation requires the same database with a distinct restricted role"))
		}
		cancellation, err := BuildCancellation(start, cancelPool, identitycancel.New(facade.New(identity)))
		if err != nil {
			return failed(err)
		}
		coordinator := coordinatedcancel.New(cancelfacade.New(cancellation))
		if c.ProviderCancelEnabled {
			providerRequests, buildErr := runapp.NewProviderCancelRequests(runpg.NewProviderCancelRequests(cancelPool), systemClock{})
			if buildErr != nil {
				return failed(buildErr)
			}
			runs, err = runapp.NewServiceWithCancellationAndProvider(repository, access, systemClock{}, coordinator, providerRequests)
		} else {
			runs, err = runapp.NewServiceWithCancellation(repository, access, systemClock{}, coordinator)
		}
		if err != nil {
			return failed(err)
		}
	}
	handler, err := runhttp.New(runs, access)
	if err != nil {
		return failed(err)
	}
	registers := []func(*gin.Engine){handler.Register}
	if c.ConsoleOIDCEnabled {
		browserSessionPool, err = database.Open(start, c.BrowserSessionDatabaseURL)
		if err != nil {
			return failed(err)
		}
		readerCfg, sessionCfg := pool.Config().ConnConfig, browserSessionPool.Config().ConnConfig
		if readerCfg.Host != sessionCfg.Host || readerCfg.Port != sessionCfg.Port || readerCfg.Database != sessionCfg.Database || readerCfg.User == sessionCfg.User {
			return failed(errors.New("browser sessions require the same database with a distinct restricted role"))
		}
		if err = migrations.Verify(start, browserSessionPool); err != nil {
			return failed(err)
		}
		if err = database.BrowserSessionRole(start, browserSessionPool); err != nil {
			return failed(err)
		}
		clientSecret, key, readErr := loadConsoleSecrets(c.OIDCClientSecretFile, c.FlowSigningKeyFile)
		if readErr != nil {
			return failed(readErr)
		}
		oidcClient, buildErr := identityoidc.New(start, identityoidc.Config{Issuer: c.OIDCIssuer, ClientID: c.OIDCClientID, ClientSecret: clientSecret, RedirectURL: c.OIDCRedirectURL})
		if buildErr != nil {
			return failed(buildErr)
		}
		sessionTokenCodec := sessioncodec.Codec{}
		sessions, buildErr := identityapp.NewHumanSessionService(identitypg.NewHumanSessions(browserSessionPool), sessionTokenCodec, systemClock{}, c.ConsoleSessionTTL)
		if buildErr != nil {
			return failed(buildErr)
		}
		login, buildErr := identityapp.NewLoginService(oidcClient, sessions, sessionTokenCodec, systemClock{})
		if buildErr != nil {
			return failed(buildErr)
		}
		flow, buildErr := identityhttp.NewFlowCookieCodec(key)
		if buildErr != nil {
			return failed(buildErr)
		}
		consoleHandler, buildErr := identityhttp.New(login, sessions, flow, c.ConsoleCookieSecure)
		if buildErr != nil {
			return failed(buildErr)
		}
		registers = append(registers, consoleHandler.Register)
		if c.ConsoleRunDelegationEnabled {
			delegations, buildErr := identityapp.NewRunDelegationService(identitypg.NewRunDelegations(browserSessionPool, browserSessionPool), sessions, sessionTokenCodec, systemClock{}, c.ConsoleRunDelegationTTL)
			if buildErr != nil {
				return failed(buildErr)
			}
			delegationFacade := facade.NewRunDelegations(delegations)
			delegationHandler, buildErr := identityhttp.NewRunDelegationHandler(sessions, delegations)
			if buildErr != nil {
				return failed(buildErr)
			}
			registers = append(registers, delegationHandler.Register)

			delegatedAccess := runidentityaccess.NewDelegated(delegationFacade)
			delegatedRuns, buildErr := runapp.NewService(repository, delegatedAccess, systemClock{})
			if buildErr != nil {
				return failed(buildErr)
			}
			if c.CoordinatedCancelEnabled {
				delegatedCancellation, cancelErr := BuildCancellation(start, cancelPool, identitycancel.NewDelegated(delegationFacade))
				if cancelErr != nil {
					return failed(cancelErr)
				}
				delegatedCoordinator := coordinatedcancel.New(cancelfacade.New(delegatedCancellation))
				if c.ProviderCancelEnabled {
					providerRequests, providerErr := runapp.NewProviderCancelRequests(runpg.NewProviderCancelRequests(cancelPool), systemClock{})
					if providerErr != nil {
						return failed(providerErr)
					}
					delegatedRuns, buildErr = runapp.NewServiceWithCancellationAndProvider(repository, delegatedAccess, systemClock{}, delegatedCoordinator, providerRequests)
				} else {
					delegatedRuns, buildErr = runapp.NewServiceWithCancellation(repository, delegatedAccess, systemClock{}, delegatedCoordinator)
				}
				if buildErr != nil {
					return failed(buildErr)
				}
			}
			delegatedRunHandler, buildErr := runhttp.New(delegatedRuns, delegatedAccess)
			if buildErr != nil {
				return failed(buildErr)
			}
			registers = append(registers, func(router *gin.Engine) { delegatedRunHandler.RegisterAt(router, "/api/console/v1") })
			delegatedQueries, queryErr := runapp.NewQueries(repository, delegatedAccess, codec, systemClock{})
			if queryErr != nil {
				return failed(queryErr)
			}
			delegatedQueryHandler, queryErr := runhttp.NewQueries(delegatedQueries, delegatedAccess)
			if queryErr != nil {
				return failed(queryErr)
			}
			registers = append(registers, func(router *gin.Engine) { delegatedQueryHandler.RegisterAt(router, "/api/console/v1") })
		}
		if c.ConsoleConnectionsEnabled {
			connectionManagerPool, buildErr = database.Open(start, c.ConnectionManagerDatabaseURL)
			if buildErr != nil {
				return failed(buildErr)
			}
			managerCfg := connectionManagerPool.Config().ConnConfig
			if readerCfg.Host != managerCfg.Host || readerCfg.Port != managerCfg.Port || readerCfg.Database != managerCfg.Database || managerCfg.User == readerCfg.User || managerCfg.User == sessionCfg.User {
				return failed(errors.New("Console Connections require the same database with a distinct restricted role"))
			}
			if buildErr = migrations.Verify(start, connectionManagerPool); buildErr != nil {
				return failed(buildErr)
			}
			if buildErr = database.ConnectionManagerRole(start, connectionManagerPool); buildErr != nil {
				return failed(buildErr)
			}
			humanAccess := connectionidentityaccess.NewHuman(facade.NewHuman(sessions))
			connectionService, serviceErr := connectionapp.NewHuman(connectionpg.New(connectionManagerPool), humanAccess)
			if serviceErr != nil {
				return failed(serviceErr)
			}
			connectionHandler, handlerErr := connectionhttp.New(connectionService, humanAccess)
			if handlerErr != nil {
				return failed(handlerErr)
			}
			registers = append(registers, connectionHandler.Register)
		}
	}
	var queries *runapp.Queries
	if c.RunReadAPIEnabled {
		queries, err = runapp.NewQueries(repository, access, codec, systemClock{})
		if err != nil {
			return failed(err)
		}
		readHandler, err := runhttp.NewQueries(queries, access)
		if err != nil {
			return failed(err)
		}
		registers = append(registers, readHandler.Register)
	}
	var admission *admissionapp.Service
	if c.StartRunAPIEnabled {
		if c.AdmissionDatabaseURL == "" {
			return failed(errors.New("StartRun API requires a separate admission database role"))
		}
		admissionPool, err = database.Open(start, c.AdmissionDatabaseURL)
		if err != nil {
			return failed(err)
		}
		readerCfg, writerCfg := pool.Config().ConnConfig, admissionPool.Config().ConnConfig
		if readerCfg.Host != writerCfg.Host || readerCfg.Port != writerCfg.Port || readerCfg.Database != writerCfg.Database || readerCfg.User == writerCfg.User {
			return failed(errors.New("admission requires the same database with a distinct restricted role"))
		}
		resolver, err := BuildAdmissionResolver(pool)
		if err != nil {
			return failed(err)
		}
		startAccess := admissionidentityaccess.New(identityFacade)
		admission, err = BuildAdmission(start, admissionPool, startAccess, resolver)
		if err != nil {
			return failed(err)
		}
		startHandler, err := admissionhttp.New(admission, startAccess)
		if err != nil {
			return failed(err)
		}
		registers = append(registers, startHandler.Register)
	}
	if c.MCPGatewayEnabled {
		bridge, buildErr := mcpapp.New(mcpidentity.New(identityFacade), mcpadmission.New(admissionfacade.NewAdmission(admission)), mcpexecution.New(runfacade.NewRuns(runs, queries)))
		if buildErr != nil {
			return failed(buildErr)
		}
		mcpHandler, buildErr := mcphttp.New(bridge)
		if buildErr != nil {
			return failed(buildErr)
		}
		registers = append(registers, func(router *gin.Engine) {
			router.Any("/mcp/v1/workspaces/:workspace_id", gin.WrapH(mcpHandler))
		})
		if c.MCPFixedToolsetEnabled {
			registry, buildErr := BuildFixedToolRegistry(pool)
			if buildErr != nil {
				return failed(buildErr)
			}
			fixedIdentity := mcpidentity.New(identityFacade)
			fixedBridge, buildErr := mcpapp.NewFixed(fixedIdentity, fixedIdentity, mcpadmission.New(admissionfacade.NewAdmission(admission)), registry)
			if buildErr != nil {
				return failed(buildErr)
			}
			fixedHandler, buildErr := mcphttp.NewFixed(fixedBridge)
			if buildErr != nil {
				return failed(buildErr)
			}
			registers = append(registers, func(router *gin.Engine) {
				router.Any("/mcp/v1/workspaces/:workspace_id/toolsets/:toolset_version_id", gin.WrapH(fixedHandler))
			})
		}
	}
	register := func(router *gin.Engine) {
		for _, register := range registers {
			register(router)
		}
	}
	ready := func(ctx context.Context) error {
		if err := migrations.Verify(ctx, pool); err != nil {
			return err
		}
		if err := database.RuntimeRole(ctx, pool); err != nil {
			return err
		}
		if cancelPool != nil {
			if err := migrations.Verify(ctx, cancelPool); err != nil {
				return err
			}
			if err := database.CancellationRole(ctx, cancelPool); err != nil {
				return err
			}
		}
		if admissionPool != nil {
			if err := migrations.Verify(ctx, admissionPool); err != nil {
				return err
			}
			return database.AdmissionRole(ctx, admissionPool)
		}
		if browserSessionPool != nil {
			if err := migrations.Verify(ctx, browserSessionPool); err != nil {
				return err
			}
			if err := database.BrowserSessionRole(ctx, browserSessionPool); err != nil {
				return err
			}
		}
		if connectionManagerPool != nil {
			if err := migrations.Verify(ctx, connectionManagerPool); err != nil {
				return err
			}
			if err := database.ConnectionManagerRole(ctx, connectionManagerPool); err != nil {
				return err
			}
		}
		return nil
	}
	return httpserver.NewConfiguredRouter(ready, register), closePools, nil
}
