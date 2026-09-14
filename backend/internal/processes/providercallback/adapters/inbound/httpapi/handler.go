package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/processes/providercallback/application"
)

type Handler struct{ service *application.Service }

func New(service *application.Service) (*Handler, error) {
	if service == nil {
		return nil, application.ErrUnavailable
	}
	return &Handler{service: service}, nil
}

func (h *Handler) Register(router *gin.Engine) {
	router.POST("/api/provider-callbacks/v1/providers/:provider_id", h.handle())
}

func failure(c *gin.Context, requestID string, status int, code, message string) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}, "meta": gin.H{"request_id": requestID}})
}

func requestID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func (h *Handler) handle() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := requestID()
		if err != nil {
			failure(c, "", http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "Request identifier unavailable.")
			return
		}
		c.Header("X-Request-ID", id)
		media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || media != "application/json" {
			failure(c, id, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Use application/json.")
			return
		}
		if c.Request.URL.RawQuery != "" {
			failure(c, id, http.StatusBadRequest, "INVALID_ARGUMENT", "Callback query parameters are not supported.")
			return
		}
		body := http.MaxBytesReader(c.Writer, c.Request.Body, application.MaxBodyBytes)
		defer body.Close()
		raw, err := io.ReadAll(body)
		if err != nil {
			var max *http.MaxBytesError
			if errors.As(err, &max) {
				failure(c, id, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "Callback body exceeds limit.")
				return
			}
			failure(c, id, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid callback body.")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		receipt, err := h.service.Handle(ctx, c.Param("provider_id"), c.GetHeader("X-Mender-Callback-Key-Id"), c.GetHeader("X-Mender-Callback-Timestamp"), c.GetHeader("X-Mender-Callback-Signature"), raw)
		if err != nil {
			switch {
			case errors.Is(err, application.ErrUnauthorized):
				failure(c, id, http.StatusUnauthorized, "INVALID_SIGNATURE", "Callback signature is invalid.")
			case errors.Is(err, application.ErrInvalid):
				failure(c, id, http.StatusBadRequest, "INVALID_CALLBACK", "Callback payload is invalid.")
			case errors.Is(err, context.DeadlineExceeded):
				failure(c, id, http.StatusGatewayTimeout, "TIMEOUT", "Callback processing timed out.")
			default:
				failure(c, id, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "Callback processing is unavailable.")
			}
			return
		}
		status := http.StatusAccepted
		if receipt.Disposition == application.Duplicate {
			status = http.StatusOK
		}
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.JSON(status, gin.H{"data": gin.H{"event_id": receipt.EventID, "disposition": receipt.Disposition}, "meta": gin.H{"request_id": id}})
	}
}
