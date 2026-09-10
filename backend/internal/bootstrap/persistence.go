package bootstrap

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	runhttp "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/httpapi"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/coordinatedcancel"
	runcursor "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/cursor"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/identityaccess"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/inbound/facade"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
	identityapp "github.com/orz-i/mender/backend/internal/contexts/identity/application"
	"github.com/orz-i/mender/backend/internal/platform/httpserver"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	cancelfacade "github.com/orz-i/mender/backend/internal/processes/admission/adapters/inbound/cancellation"
	"github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/identitycancel"
	"github.com/orz-i/mender/backend/migrations"
)

type APIConfig struct {
	RunAPIEnabled            bool
	RunReadAPIEnabled        bool
	DatabaseURL              string
	CursorSigningKey         []byte
	CoordinatedCancelEnabled bool
	CancellationDatabaseURL  string
}

func LoadAPIConfig(getenv func(string) string) (APIConfig, error) {
	var c APIConfig
	switch getenv("MENDER_RUN_COORDINATED_CANCEL_ENABLED") {
	case "", "false":
	case "true":
		c.CoordinatedCancelEnabled = true
	default:
		return c, errors.New("MENDER_RUN_COORDINATED_CANCEL_ENABLED must be true or false")
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
	switch getenv("MENDER_RUN_API_ENABLED") {
	case "", "false":
		if c.RunReadAPIEnabled || c.CoordinatedCancelEnabled {
			return APIConfig{}, errors.New("Run read API requires the authenticated Run API")
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
		if c.RunReadAPIEnabled || c.CoordinatedCancelEnabled {
			return nil, nil, errors.New("Run read API requires the authenticated Run API")
		}
		return httpserver.NewRouter(), func() {}, nil
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
	closePools := func() {
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
	access := identityaccess.New(facade.New(identity))
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
		runs, err = runapp.NewServiceWithCancellation(repository, access, systemClock{}, coordinatedcancel.New(cancelfacade.New(cancellation)))
		if err != nil {
			return failed(err)
		}
	}
	handler, err := runhttp.New(runs, access)
	if err != nil {
		return failed(err)
	}
	register := handler.Register
	if c.RunReadAPIEnabled {
		queries, err := runapp.NewQueries(repository, access, codec, systemClock{})
		if err != nil {
			return failed(err)
		}
		readHandler, err := runhttp.NewQueries(queries, access)
		if err != nil {
			return failed(err)
		}
		register = func(router *gin.Engine) { handler.Register(router); readHandler.Register(router) }
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
			return database.CancellationRole(ctx, cancelPool)
		}
		return nil
	}
	return httpserver.NewConfiguredRouter(ready, register), closePools, nil
}
