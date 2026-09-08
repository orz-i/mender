package httpserver

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type healthResponse struct {
	Service string `json:"service"`
	Status  string `json:"status"`
	Stage   string `json:"stage"`
}

func NewRouter() http.Handler {
	router := gin.New()
	router.Use(gin.Recovery())
	// Probes do not use forwarded client IP headers.
	_ = router.SetTrustedProxies(nil)
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, healthResponse{Service: "mender-api", Status: "ok", Stage: "bootstrap"})
	})
	// Liveness is available; business readiness must wait for identity and storage.
	router.GET("/readyz", func(c *gin.Context) {
		c.JSON(http.StatusServiceUnavailable, healthResponse{Service: "mender-api", Status: "not_ready", Stage: "bootstrap"})
	})
	return router
}
