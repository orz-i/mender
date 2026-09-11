package bootstrap

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	runfacade "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/facade"
	runhttp "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/httpapi"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/coordinatedcancel"
	runcursor "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/cursor"
	runidentityaccess "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/identityaccess"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/inbound/facade"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
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
	RunAPIEnabled            bool
	RunReadAPIEnabled        bool
	DatabaseURL              string
	CursorSigningKey         []byte
	CoordinatedCancelEnabled bool
	ProviderCancelEnabled    bool
	CancellationDatabaseURL  string
	StartRunAPIEnabled       bool
	AdmissionDatabaseURL     string
	MCPGatewayEnabled        bool
	MCPFixedToolsetEnabled   bool
}

func LoadAPIConfig(getenv func(string) string) (APIConfig, error) {
	var c APIConfig
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
		if c.RunReadAPIEnabled || c.CoordinatedCancelEnabled || c.ProviderCancelEnabled || c.StartRunAPIEnabled || c.MCPGatewayEnabled || c.MCPFixedToolsetEnabled {
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
		if c.RunReadAPIEnabled || c.CoordinatedCancelEnabled || c.ProviderCancelEnabled || c.StartRunAPIEnabled || c.MCPGatewayEnabled || c.MCPFixedToolsetEnabled {
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
	closePools := func() {
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
			fixedBridge, buildErr := mcpapp.NewFixed(mcpidentity.New(identityFacade), mcpadmission.New(admissionfacade.NewAdmission(admission)), registry)
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
		return nil
	}
	return httpserver.NewConfiguredRouter(ready, register), closePools, nil
}
