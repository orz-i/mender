package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

const (
	publicationSessionCookie = "mender_session"
	publicationMaxBody       = 1 << 20
)

type PublicationHandler struct {
	service    *application.PublisherService
	authorizer application.PublisherAuthorizer
}

func NewPublication(service *application.PublisherService, authorizer application.PublisherAuthorizer) (*PublicationHandler, error) {
	if service == nil || authorizer == nil {
		return nil, application.ErrPublicationUnavailable
	}
	return &PublicationHandler{service: service, authorizer: authorizer}, nil
}

func (h *PublicationHandler) Register(router *gin.Engine) {
	base := "/api/console/v1/workspaces/:workspace_id/publisher"
	router.GET(base, h.snapshot)
	router.POST(base+"/publishers", h.createPublisher)
	router.PUT(base+"/publishers/:publisher_id", h.updatePublisher)
	router.POST(base+"/publishers/:publisher_id/plugins", h.createPlugin)
	router.POST(base+"/plugins/:plugin_id/versions", h.createVersion)
	router.PUT(base+"/plugins/:plugin_id/versions/:version", h.updateVersion)
	router.POST(base+"/plugins/:plugin_id/versions/:version/preflight", h.preflight)
}

func publicationConfigure(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
}

func publicationFail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, application.ErrPublicationUnauthenticated):
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHENTICATED", "message": "Login is required."}})
	case errors.Is(err, application.ErrPublicationForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "Publisher operation is not permitted."}})
	case errors.Is(err, application.ErrPublicationInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Publisher request is invalid."}})
	case errors.Is(err, application.ErrPublicationNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "NOT_FOUND", "message": "Publisher record was not found."}})
	case errors.Is(err, application.ErrPublicationConflict):
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "CONFLICT", "message": "Publisher state changed or publication requirements are not satisfied."}})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": gin.H{"code": "TIMEOUT", "message": "Request deadline exceeded."}})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "PUBLISHER_UNAVAILABLE", "message": "Publisher management is temporarily unavailable."}})
	}
}

func publicationValidID(value string) bool {
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

func publicationDecode(c *gin.Context, dst any) error {
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > publicationMaxBody {
		return application.ErrPublicationInvalid
	}
	dec := json.NewDecoder(io.LimitReader(c.Request.Body, publicationMaxBody+1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return application.ErrPublicationInvalid
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return application.ErrPublicationInvalid
	}
	return nil
}

func publicationEmptyMutation(c *gin.Context) bool {
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 0 {
		publicationFail(c, application.ErrPublicationInvalid)
		return false
	}
	return true
}

func (h *PublicationHandler) actor(c *gin.Context, mutation bool) (context.Context, context.CancelFunc, application.PublisherActor, bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	raw, err := c.Cookie(publicationSessionCookie)
	if err != nil {
		publicationFail(c, application.ErrPublicationUnauthenticated)
		return ctx, cancel, application.PublisherActor{}, false
	}
	var actor application.PublisherActor
	if mutation {
		csrf := c.GetHeader("X-Mender-CSRF")
		if csrf == "" || len(csrf) > 256 {
			publicationFail(c, application.ErrPublicationForbidden)
			return ctx, cancel, application.PublisherActor{}, false
		}
		actor, err = h.authorizer.AuthenticateMutation(ctx, raw, csrf)
	} else {
		actor, err = h.authorizer.Authenticate(ctx, raw)
	}
	if err != nil {
		publicationFail(c, err)
		return ctx, cancel, application.PublisherActor{}, false
	}
	return ctx, cancel, actor, true
}

type publisherInput struct {
	PublisherID string `json:"publisher_id"`
	DisplayName string `json:"display_name"`
}

type publisherUpdateInput struct {
	DisplayName string `json:"display_name"`
}

type pluginInput struct {
	PluginID string `json:"plugin_id"`
}

func publisherView(value application.Publisher, actor application.PublisherActor) gin.H {
	return gin.H{
		"publisher_id": value.ID,
		"display_name": value.DisplayName,
		"state":        value.State,
		"owned":        value.OwnerUserID == actor.UserID,
		"created_at":   value.CreatedAt,
		"updated_at":   value.UpdatedAt,
	}
}

func pluginView(value application.Plugin) gin.H {
	return gin.H{
		"plugin_id":    value.ID,
		"publisher_id": value.PublisherID,
		"created_at":   value.CreatedAt,
	}
}

func nullablePublicationTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func pluginVersionView(value application.PluginVersion) gin.H {
	return gin.H{
		"plugin_id":       value.PluginID,
		"version":         value.Version,
		"publisher_id":    value.PublisherID,
		"revision":        strconv.FormatInt(value.Revision, 10),
		"state":           value.State,
		"manifest":        value.Manifest,
		"manifest_sha256": value.ManifestSHA256,
		"created_at":      value.CreatedAt,
		"updated_at":      value.UpdatedAt,
		"submitted_at":    nullablePublicationTime(value.SubmittedAt),
		"approved_at":     nullablePublicationTime(value.ApprovedAt),
		"published_at":    nullablePublicationTime(value.PublishedAt),
		"deprecated_at":   nullablePublicationTime(value.DeprecatedAt),
		"disabled_at":     nullablePublicationTime(value.DisabledAt),
	}
}

func preflightView(value application.PluginPreflight) gin.H {
	issues := make([]gin.H, 0, len(value.Issues))
	for _, issue := range value.Issues {
		issues = append(issues, gin.H{"code": issue.Code, "target_id": issue.TargetID})
	}
	return gin.H{"ready": value.Ready, "issues": issues}
}

func (h *PublicationHandler) snapshot(c *gin.Context) {
	publicationConfigure(c)
	workspace := c.Param("workspace_id")
	if c.Request.URL.RawQuery != "" || !publicationValidID(workspace) {
		publicationFail(c, application.ErrPublicationInvalid)
		return
	}
	ctx, cancel, actor, ok := h.actor(c, false)
	defer cancel()
	if !ok {
		return
	}
	value, err := h.service.Snapshot(ctx, actor, workspace)
	if err != nil {
		publicationFail(c, err)
		return
	}
	publishers := make([]gin.H, 0, len(value.Publishers))
	for _, item := range value.Publishers {
		publishers = append(publishers, publisherView(item, actor))
	}
	plugins := make([]gin.H, 0, len(value.Plugins))
	for _, item := range value.Plugins {
		plugins = append(plugins, pluginView(item))
	}
	versions := make([]gin.H, 0, len(value.PluginVersions))
	for _, item := range value.PluginVersions {
		versions = append(versions, pluginVersionView(item))
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"publishers": publishers, "plugins": plugins, "plugin_versions": versions}})
}

func (h *PublicationHandler) createPublisher(c *gin.Context) {
	publicationConfigure(c)
	var input publisherInput
	if err := publicationDecode(c, &input); err != nil {
		publicationFail(c, err)
		return
	}
	ctx, cancel, actor, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	value, err := h.service.CreatePublisher(ctx, actor, c.Param("workspace_id"), input.PublisherID, input.DisplayName)
	if err != nil {
		publicationFail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": publisherView(value, actor)})
}

func (h *PublicationHandler) updatePublisher(c *gin.Context) {
	publicationConfigure(c)
	var input publisherUpdateInput
	if err := publicationDecode(c, &input); err != nil {
		publicationFail(c, err)
		return
	}
	ctx, cancel, actor, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	value, err := h.service.UpdatePublisher(ctx, actor, c.Param("workspace_id"), c.Param("publisher_id"), input.DisplayName)
	if err != nil {
		publicationFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": publisherView(value, actor)})
}

func (h *PublicationHandler) createPlugin(c *gin.Context) {
	publicationConfigure(c)
	var input pluginInput
	if err := publicationDecode(c, &input); err != nil {
		publicationFail(c, err)
		return
	}
	ctx, cancel, actor, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	value, err := h.service.CreatePlugin(ctx, actor, c.Param("workspace_id"), c.Param("publisher_id"), input.PluginID)
	if err != nil {
		publicationFail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": pluginView(value)})
}

func (h *PublicationHandler) createVersion(c *gin.Context) {
	publicationConfigure(c)
	var manifest application.PluginManifest
	if err := publicationDecode(c, &manifest); err != nil {
		publicationFail(c, err)
		return
	}
	if manifest.PluginID != c.Param("plugin_id") {
		publicationFail(c, application.ErrPublicationInvalid)
		return
	}
	ctx, cancel, actor, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	value, err := h.service.CreatePluginVersion(ctx, actor, c.Param("workspace_id"), manifest)
	if err != nil {
		publicationFail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": pluginVersionView(value)})
}

func (h *PublicationHandler) updateVersion(c *gin.Context) {
	publicationConfigure(c)
	var manifest application.PluginManifest
	if err := publicationDecode(c, &manifest); err != nil {
		publicationFail(c, err)
		return
	}
	if manifest.PluginID != c.Param("plugin_id") || manifest.Version != c.Param("version") {
		publicationFail(c, application.ErrPublicationInvalid)
		return
	}
	ctx, cancel, actor, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	value, err := h.service.UpdatePluginVersion(ctx, actor, c.Param("workspace_id"), c.Param("plugin_id"), c.Param("version"), manifest)
	if err != nil {
		publicationFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": pluginVersionView(value)})
}

func (h *PublicationHandler) preflight(c *gin.Context) {
	publicationConfigure(c)
	if !publicationEmptyMutation(c) {
		return
	}
	ctx, cancel, actor, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	value, err := h.service.PluginVersionPreflight(ctx, actor, c.Param("workspace_id"), c.Param("plugin_id"), c.Param("version"))
	if err != nil {
		publicationFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": preflightView(value)})
}
