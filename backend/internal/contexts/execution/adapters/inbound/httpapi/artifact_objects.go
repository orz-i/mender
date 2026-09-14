package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
)

type ArtifactObjectHandler struct {
	access        *application.ArtifactObjectAccess
	authenticator ports.Authenticator
}

func statusExpiresAt(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func (h *ArtifactObjectHandler) status(c *gin.Context) {
	ctx, done, caller, requestID, ok := authenticateRequest(c, h.authenticator)
	defer done()
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" {
		failError(c, requestID, application.ErrInvalidRequest)
		return
	}
	status, err := h.access.Status(ctx, caller, ports.RunID(c.Param("run_id")), c.Param("artifact_id"))
	if err != nil {
		switch {
		case errors.Is(err, application.ErrArtifactCapabilityInvalid):
			failError(c, requestID, application.ErrInvalidRequest)
		default:
			failError(c, requestID, err)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"state": status.State, "expires_at": statusExpiresAt(status.ExpiresAt)}, "meta": gin.H{"request_id": requestID}})
}

func NewArtifactObjects(access *application.ArtifactObjectAccess, authenticator ports.Authenticator) (*ArtifactObjectHandler, error) {
	if access == nil || authenticator == nil {
		return nil, errors.New("artifact object HTTP adapter requires access service and authentication")
	}
	return &ArtifactObjectHandler{access: access, authenticator: authenticator}, nil
}

func (h *ArtifactObjectHandler) RegisterProtectedAt(router *gin.Engine, prefix string) {
	base := prefix + "/workspaces/:workspace_id/runs/:run_id/artifacts/:artifact_id/object"
	router.GET(base, h.status)
	router.POST(base+"-capability", h.issue)
}

func (h *ArtifactObjectHandler) RegisterSigned(router *gin.Engine) {
	router.GET("/api/v1/artifact-objects/content", h.read)
}

func bodyIsEmpty(c *gin.Context) bool {
	if c.Request.Body == nil {
		return true
	}
	if c.Request.ContentLength > 0 || len(c.Request.TransferEncoding) > 0 {
		return false
	}
	var one [1]byte
	n, err := c.Request.Body.Read(one[:])
	return n == 0 && errors.Is(err, io.EOF)
}

func (h *ArtifactObjectHandler) issue(c *gin.Context) {
	ctx, done, caller, requestID, ok := authenticateRequest(c, h.authenticator)
	defer done()
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" || !bodyIsEmpty(c) {
		failError(c, requestID, application.ErrInvalidRequest)
		return
	}
	grant, err := h.access.Issue(ctx, caller, ports.RunID(c.Param("run_id")), c.Param("artifact_id"))
	if err != nil {
		switch {
		case errors.Is(err, application.ErrArtifactObjectNotFound):
			failure(c, requestID, http.StatusNotFound, "ARTIFACT_OBJECT_NOT_FOUND", "Artifact has no available object copy.")
		case errors.Is(err, application.ErrArtifactObjectExpired):
			failure(c, requestID, http.StatusGone, "ARTIFACT_OBJECT_EXPIRED", "Artifact object copy is no longer available.")
		case errors.Is(err, application.ErrArtifactCapabilityInvalid):
			failError(c, requestID, application.ErrInvalidRequest)
		default:
			failError(c, requestID, err)
		}
		return
	}
	signedURL := "/api/v1/artifact-objects/content?cap=" + url.QueryEscape(grant.Token)
	c.JSON(http.StatusCreated, gin.H{"data": gin.H{"url": signedURL, "expires_at": grant.ExpiresAt}, "meta": gin.H{"request_id": requestID}})
}

func signedRequestContext(c *gin.Context) (context.Context, context.CancelFunc, string, bool) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	ctx, done := context.WithTimeout(c.Request.Context(), 5*time.Second)
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		failure(c, "", http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "Request identifier unavailable.")
		return ctx, done, "", false
	}
	requestID := hex.EncodeToString(random[:])
	c.Header("X-Request-ID", requestID)
	return ctx, done, requestID, true
}

func exactCapabilityQuery(raw string) (string, error) {
	if len(raw) < 1 || len(raw) > 4096 {
		return "", application.ErrArtifactCapabilityInvalid
	}
	values, err := url.ParseQuery(raw)
	if err != nil || len(values) != 1 {
		return "", application.ErrArtifactCapabilityInvalid
	}
	capability := values["cap"]
	if len(capability) != 1 || capability[0] == "" || len(capability[0]) > 2048 {
		return "", application.ErrArtifactCapabilityInvalid
	}
	return capability[0], nil
}

func (h *ArtifactObjectHandler) read(c *gin.Context) {
	ctx, done, requestID, ok := signedRequestContext(c)
	defer done()
	if !ok {
		return
	}
	token, err := exactCapabilityQuery(c.Request.URL.RawQuery)
	if err != nil {
		failure(c, requestID, http.StatusNotFound, "OBJECT_CAPABILITY_INVALID", "Artifact object capability is invalid or expired.")
		return
	}
	content, _, err := h.access.Read(ctx, token)
	if err != nil {
		switch {
		case errors.Is(err, application.ErrArtifactCapabilityInvalid), errors.Is(err, application.ErrArtifactObjectNotFound):
			failure(c, requestID, http.StatusNotFound, "OBJECT_CAPABILITY_INVALID", "Artifact object capability is invalid or expired.")
		case errors.Is(err, application.ErrArtifactObjectExpired):
			failure(c, requestID, http.StatusGone, "ARTIFACT_OBJECT_EXPIRED", "Artifact object copy is no longer available.")
		default:
			failError(c, requestID, err)
		}
		return
	}
	c.Data(http.StatusOK, "application/json", content)
}
