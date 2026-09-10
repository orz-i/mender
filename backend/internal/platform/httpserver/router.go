package httpserver

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type healthResponse struct {
	Service string `json:"service"`
	Status  string `json:"status"`
	Stage   string `json:"stage"`
}

func NewRouter() http.Handler {
	return NewConfiguredRouter(nil, nil)
}

// Registration and readiness are injected by bootstrap; platform imports no business context.
func NewConfiguredRouter(ready func(context.Context) error, register func(*gin.Engine)) http.Handler {
	router := gin.New()
	router.Use(gin.Recovery())
	// Probes do not use forwarded client IP headers.
	_ = router.SetTrustedProxies(nil)
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, healthResponse{Service: "mender-api", Status: "ok", Stage: "bootstrap"})
	})
	// Liveness is available; business readiness must wait for identity and storage.
	router.GET("/readyz", func(c *gin.Context) {
		if ready != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			defer cancel()
			if ready(ctx) == nil {
				c.JSON(http.StatusOK, healthResponse{Service: "mender-api", Status: "ready", Stage: "run-query-cancel"})
				return
			}
		}
		c.JSON(http.StatusServiceUnavailable, healthResponse{Service: "mender-api", Status: "not_ready", Stage: "bootstrap"})
	})
	if register != nil {
		register(router)
	}
	return router
}
