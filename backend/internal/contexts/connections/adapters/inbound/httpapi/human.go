package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/connections/application"
)

const sessionCookie = "mender_session"

type Handler struct {
	service    *application.HumanService
	authorizer application.HumanAuthorizer
}

func New(service *application.HumanService, authorizer application.HumanAuthorizer) (*Handler, error) {
	if service == nil || authorizer == nil {
		return nil, errors.New("human connection HTTP adapter is not configured")
	}
	return &Handler{service: service, authorizer: authorizer}, nil
}

func (h *Handler) Register(router *gin.Engine) {
	base := "/api/console/v1/workspaces/:workspace_id/connections"
	router.GET(base, h.list)
	router.DELETE(base+"/:connection_id", h.revoke)
}

func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, application.ErrUnauthenticated):
		c.JSON(401, gin.H{"error": gin.H{"code": "UNAUTHENTICATED", "message": "Login is required."}})
	case errors.Is(err, application.ErrForbidden):
		c.JSON(403, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "Connection operation is not permitted."}})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(504, gin.H{"error": gin.H{"code": "TIMEOUT", "message": "Request deadline exceeded."}})
	default:
		c.JSON(503, gin.H{"error": gin.H{"code": "CONNECTIONS_UNAVAILABLE", "message": "Connections are temporarily unavailable."}})
	}
}

func configure(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
}

func validID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func (h *Handler) actor(c *gin.Context, mutation bool) (context.Context, context.CancelFunc, application.HumanActor, bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	raw, err := c.Cookie(sessionCookie)
	if err != nil {
		fail(c, application.ErrUnauthenticated)
		return ctx, cancel, application.HumanActor{}, false
	}
	var actor application.HumanActor
	if mutation {
		csrf := c.GetHeader("X-Mender-CSRF")
		if csrf == "" || len(csrf) > 256 {
			fail(c, application.ErrForbidden)
			return ctx, cancel, application.HumanActor{}, false
		}
		actor, err = h.authorizer.AuthenticateMutation(ctx, raw, csrf)
	} else {
		actor, err = h.authorizer.Authenticate(ctx, raw)
	}
	if err != nil {
		fail(c, err)
		return ctx, cancel, application.HumanActor{}, false
	}
	return ctx, cancel, actor, true
}

type connectionDTO struct {
	ConnectionID string    `json:"connection_id"`
	ProviderID   string    `json:"provider_id"`
	State        string    `json:"state"`
	Revision     int64     `json:"revision"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func dto(item application.HumanConnection) connectionDTO {
	return connectionDTO{ConnectionID: item.ConnectionID, ProviderID: item.ProviderID, State: item.State, Revision: item.Revision, CreatedAt: item.CreatedAt, ExpiresAt: item.ExpiresAt}
}

func (h *Handler) list(c *gin.Context) {
	configure(c)
	if c.Request.URL.RawQuery != "" {
		c.JSON(400, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Connection query is invalid."}})
		return
	}
	workspace := c.Param("workspace_id")
	if !validID(workspace) {
		c.JSON(400, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Workspace is invalid."}})
		return
	}
	ctx, cancel, actor, ok := h.actor(c, false)
	defer cancel()
	if !ok {
		return
	}
	items, err := h.service.List(ctx, actor, workspace)
	if err != nil {
		fail(c, err)
		return
	}
	result := make([]connectionDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto(item))
	}
	c.JSON(200, gin.H{"data": result})
}

func (h *Handler) revoke(c *gin.Context) {
	configure(c)
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 0 {
		c.JSON(400, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Connection revoke request is invalid."}})
		return
	}
	workspace, connectionID := c.Param("workspace_id"), c.Param("connection_id")
	if !validID(workspace) || !validID(connectionID) {
		c.JSON(400, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Connection target is invalid."}})
		return
	}
	ctx, cancel, actor, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	item, err := h.service.Revoke(ctx, actor, workspace, connectionID, time.Now().UTC())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": dto(item)})
}
