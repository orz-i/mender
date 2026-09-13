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
	"github.com/orz-i/mender/backend/internal/processes/catalogmanagement/application"
)

const sessionCookie = "mender_session"
const maxBody = 2 << 20

type Handler struct {
	service *application.Service
	auth    application.Authorizer
}

func (h *Handler) requestToolsetReview(c *gin.Context) {
	configure(c)
	if !emptyMutation(c) {
		return
	}
	ctx, cancel, a, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.SubmitPublication(ctx, a, c.Param("workspace_id"), "toolset", c.Param("toolset_id"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": approvalView(v)})
}
func (h *Handler) requestToolReview(c *gin.Context) {
	configure(c)
	if !emptyMutation(c) {
		return
	}
	ctx, cancel, a, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.SubmitPublication(ctx, a, c.Param("workspace_id"), "tool_version", c.Param("tool_version_id"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": approvalView(v)})
}

func New(service *application.Service, auth application.Authorizer) (*Handler, error) {
	if service == nil || auth == nil {
		return nil, application.ErrUnavailable
	}
	return &Handler{service: service, auth: auth}, nil
}
func (h *Handler) Register(router *gin.Engine) {
	base := "/api/console/v1/workspaces/:workspace_id/catalog"
	router.GET(base, h.snapshot)
	router.POST(base+"/tool-versions", h.createTool)
	router.PUT(base+"/tool-versions/:tool_version_id", h.updateTool)
	router.POST(base+"/tool-versions/:tool_version_id/preflight", h.toolPreflight)
	router.POST(base+"/tool-versions/:tool_version_id/review-requests", h.requestToolReview)
	router.POST(base+"/tool-versions/:tool_version_id/publish", h.publishTool)
	router.POST(base+"/tool-versions/:tool_version_id/retire", h.retireTool)
	router.POST(base+"/toolsets", h.createToolset)
	router.PUT(base+"/toolsets/:toolset_id/bindings/:tool_version_id", h.upsertBinding)
	router.DELETE(base+"/toolsets/:toolset_id/bindings/:tool_version_id", h.deleteBinding)
	router.POST(base+"/toolsets/:toolset_id/preflight", h.toolsetPreflight)
	router.POST(base+"/toolsets/:toolset_id/review-requests", h.requestToolsetReview)
	router.POST(base+"/toolsets/:toolset_id/publish", h.publishToolset)
	router.POST(base+"/toolsets/:toolset_id/retire", h.retireToolset)
}

func configure(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
}
func validID(v string) bool {
	if len(v) < 1 || len(v) > 128 {
		return false
	}
	for _, ch := range v {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}
func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, application.ErrUnauthenticated):
		c.JSON(401, gin.H{"error": gin.H{"code": "UNAUTHENTICATED", "message": "Login is required."}})
	case errors.Is(err, application.ErrForbidden):
		c.JSON(403, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "Catalog operation is not permitted."}})
	case errors.Is(err, application.ErrInvalid):
		c.JSON(400, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Catalog request is invalid."}})
	case errors.Is(err, application.ErrNotFound):
		c.JSON(404, gin.H{"error": gin.H{"code": "NOT_FOUND", "message": "Catalog record was not found."}})
	case errors.Is(err, application.ErrConflict):
		c.JSON(409, gin.H{"error": gin.H{"code": "CONFLICT", "message": "Catalog state changed or publication requirements are not satisfied."}})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(504, gin.H{"error": gin.H{"code": "TIMEOUT", "message": "Request deadline exceeded."}})
	default:
		c.JSON(503, gin.H{"error": gin.H{"code": "CATALOG_UNAVAILABLE", "message": "Catalog management is temporarily unavailable."}})
	}
}
func (h *Handler) actor(c *gin.Context, mutation bool) (context.Context, context.CancelFunc, application.Actor, bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	raw, err := c.Cookie(sessionCookie)
	if err != nil {
		fail(c, application.ErrUnauthenticated)
		return ctx, cancel, application.Actor{}, false
	}
	var a application.Actor
	if mutation {
		csrf := c.GetHeader("X-Mender-CSRF")
		if csrf == "" || len(csrf) > 256 {
			fail(c, application.ErrForbidden)
			return ctx, cancel, application.Actor{}, false
		}
		a, err = h.auth.AuthenticateMutation(ctx, raw, csrf)
	} else {
		a, err = h.auth.Authenticate(ctx, raw)
	}
	if err != nil {
		fail(c, err)
		return ctx, cancel, application.Actor{}, false
	}
	return ctx, cancel, a, true
}
func decode(c *gin.Context, dst any) error {
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > maxBody {
		return application.ErrInvalid
	}
	dec := json.NewDecoder(io.LimitReader(c.Request.Body, maxBody+1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return application.ErrInvalid
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return application.ErrInvalid
	}
	return nil
}
func emptyMutation(c *gin.Context) bool {
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 0 {
		fail(c, application.ErrInvalid)
		return false
	}
	return true
}

type toolInput struct {
	ToolVersionID      string          `json:"tool_version_id"`
	ToolID             string          `json:"tool_id"`
	Version            string          `json:"version"`
	ProviderID         string          `json:"provider_id"`
	PriceVersionID     string          `json:"price_version_id"`
	DeploymentRevision string          `json:"deployment_revision"`
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	InputSchema        json.RawMessage `json:"input_schema"`
	OutputSchema       json.RawMessage `json:"output_schema"`
	SideEffect         string          `json:"side_effect"`
	Idempotency        string          `json:"idempotency"`
	MCPPublishable     bool            `json:"mcp_publishable"`
}

func (v toolInput) app() application.ToolVersionInput {
	return application.ToolVersionInput{ToolVersionID: v.ToolVersionID, ToolID: v.ToolID, Version: v.Version, ProviderID: v.ProviderID, PriceVersionID: v.PriceVersionID, DeploymentRevision: v.DeploymentRevision, Title: v.Title, Description: v.Description, InputSchema: string(v.InputSchema), OutputSchema: string(v.OutputSchema), SideEffect: v.SideEffect, Idempotency: v.Idempotency, MCPPublishable: v.MCPPublishable}
}

type bindingInput struct {
	ToolID           string `json:"tool_id"`
	ToolVersionLabel string `json:"tool_version_label"`
	BudgetID         string `json:"budget_id"`
	ConnectionID     string `json:"connection_id"`
	MCPName          string `json:"mcp_name"`
	MCPExposed       bool   `json:"mcp_exposed"`
}
type toolsetInput struct {
	ID string `json:"id"`
}

func toolView(v application.ToolVersion) gin.H {
	return gin.H{"tool_version_id": v.ToolVersionID, "tool_id": v.ToolID, "version": v.Version, "provider_id": v.ProviderID, "price_version_id": v.PriceVersionID, "deployment_revision": v.DeploymentRevision, "title": v.Title, "description": v.Description, "input_schema": json.RawMessage(v.InputSchema), "output_schema": json.RawMessage(v.OutputSchema), "side_effect": v.SideEffect, "idempotency": v.Idempotency, "mcp_publishable": v.MCPPublishable, "revision": strconv.FormatInt(v.Revision, 10), "state": v.State, "created_at": v.CreatedAt, "updated_at": v.UpdatedAt, "published_at": nullableTime(v.PublishedAt), "retired_at": nullableTime(v.RetiredAt)}
}
func bindingView(v application.Binding) gin.H {
	return gin.H{"tool_id": v.ToolID, "tool_version_label": v.ToolVersionLabel, "tool_version_id": v.ToolVersionID, "budget_id": v.BudgetID, "connection_id": v.ConnectionID, "mcp_name": v.MCPName, "mcp_exposed": v.MCPExposed, "state": v.State, "published_at": nullableTime(v.PublishedAt)}
}
func toolsetView(v application.Toolset) gin.H {
	bindings := make([]gin.H, 0, len(v.Bindings))
	for _, b := range v.Bindings {
		bindings = append(bindings, bindingView(b))
	}
	return gin.H{"id": v.ID, "revision": strconv.FormatInt(v.Revision, 10), "state": v.State, "created_at": v.CreatedAt, "updated_at": v.UpdatedAt, "published_at": nullableTime(v.PublishedAt), "retired_at": nullableTime(v.RetiredAt), "bindings": bindings}
}
func approvalView(v application.PublicationApproval) gin.H {
	return gin.H{"id": v.ID, "target_kind": v.TargetKind, "target_id": v.TargetID, "target_revision": strconv.FormatInt(v.TargetRevision, 10), "requester_user_id": v.RequesterUserID, "state": v.State, "requested_at": v.RequestedAt, "expires_at": v.ExpiresAt, "reviewer_user_id": nullableString(v.ReviewerUserID), "reviewed_at": nullableTime(v.ReviewedAt), "decision_note": v.DecisionNote, "consumed_at": nullableTime(v.ConsumedAt)}
}
func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func nullableTime(v time.Time) any {
	if v.IsZero() {
		return nil
	}
	return v
}
func preflightView(v application.Preflight) gin.H {
	issues := make([]gin.H, 0, len(v.Issues))
	for _, i := range v.Issues {
		issues = append(issues, gin.H{"code": i.Code, "target_id": i.TargetID})
	}
	return gin.H{"ready": v.Ready, "issues": issues}
}

func (h *Handler) snapshot(c *gin.Context) {
	configure(c)
	if c.Request.URL.RawQuery != "" {
		fail(c, application.ErrInvalid)
		return
	}
	ws := c.Param("workspace_id")
	if !validID(ws) {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, a, ok := h.actor(c, false)
	defer cancel()
	if !ok {
		return
	}
	s, err := h.service.Snapshot(ctx, a, ws)
	if err != nil {
		fail(c, err)
		return
	}
	tools := make([]gin.H, 0, len(s.ToolVersions))
	for _, v := range s.ToolVersions {
		tools = append(tools, toolView(v))
	}
	sets := make([]gin.H, 0, len(s.Toolsets))
	for _, v := range s.Toolsets {
		sets = append(sets, toolsetView(v))
	}
	connections := make([]gin.H, 0, len(s.Connections))
	for _, v := range s.Connections {
		connections = append(connections, gin.H{"connection_id": v.ConnectionID, "provider_id": v.ProviderID, "state": v.State, "revision": strconv.FormatInt(v.Revision, 10), "created_at": v.CreatedAt, "expires_at": v.ExpiresAt})
	}
	prices := make([]gin.H, 0, len(s.Prices))
	for _, v := range s.Prices {
		prices = append(prices, gin.H{"id": v.ID, "tool_version_id": v.ToolVersionID, "currency": v.Currency, "reserve_micro": strconv.FormatInt(v.ReserveMicro, 10), "starts_at": v.StartsAt, "ends_at": v.EndsAt, "active": v.Active})
	}
	budgets := make([]gin.H, 0, len(s.Budgets))
	for _, v := range s.Budgets {
		budgets = append(budgets, gin.H{"budget_id": v.BudgetID, "period_id": v.PeriodID, "currency": v.Currency, "starts_at": v.StartsAt, "ends_at": v.EndsAt, "active": v.Active})
	}
	approvals := make([]gin.H, 0, len(s.Approvals))
	for _, v := range s.Approvals {
		approvals = append(approvals, approvalView(v))
	}
	c.JSON(200, gin.H{"data": gin.H{"tool_versions": tools, "toolsets": sets, "connections": connections, "price_versions": prices, "budget_periods": budgets, "publication_approvals": approvals}})
}
func (h *Handler) createTool(c *gin.Context) {
	configure(c)
	var in toolInput
	if err := decode(c, &in); err != nil {
		fail(c, err)
		return
	}
	ctx, cancel, a, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.CreateToolVersion(ctx, a, c.Param("workspace_id"), in.app())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": toolView(v)})
}
func (h *Handler) updateTool(c *gin.Context) {
	configure(c)
	var in toolInput
	if err := decode(c, &in); err != nil {
		fail(c, err)
		return
	}
	ctx, cancel, a, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.UpdateToolVersion(ctx, a, c.Param("workspace_id"), c.Param("tool_version_id"), in.app())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": toolView(v)})
}
func (h *Handler) toolPreflight(c *gin.Context) {
	configure(c)
	if !emptyMutation(c) {
		return
	}
	ctx, cancel, a, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.ToolVersionPreflight(ctx, a, c.Param("workspace_id"), c.Param("tool_version_id"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": preflightView(v)})
}
func (h *Handler) publishTool(c *gin.Context) {
	configure(c)
	if !emptyMutation(c) {
		return
	}
	ctx, cancel, a, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.PublishToolVersion(ctx, a, c.Param("workspace_id"), c.Param("tool_version_id"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": toolView(v)})
}
func (h *Handler) retireTool(c *gin.Context) {
	configure(c)
	if !emptyMutation(c) {
		return
	}
	ctx, cancel, a, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.RetireToolVersion(ctx, a, c.Param("workspace_id"), c.Param("tool_version_id"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": toolView(v)})
}
func (h *Handler) createToolset(c *gin.Context) {
	configure(c)
	var in toolsetInput
	if err := decode(c, &in); err != nil {
		fail(c, err)
		return
	}
	ctx, cancel, a, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.CreateToolset(ctx, a, c.Param("workspace_id"), in.ID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": toolsetView(v)})
}
func (h *Handler) upsertBinding(c *gin.Context) {
	configure(c)
	var in bindingInput
	if err := decode(c, &in); err != nil {
		fail(c, err)
		return
	}
	ctx, cancel, a, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	app := application.BindingInput{ToolID: in.ToolID, ToolVersionLabel: in.ToolVersionLabel, ToolVersionID: c.Param("tool_version_id"), BudgetID: in.BudgetID, ConnectionID: in.ConnectionID, MCPName: in.MCPName, MCPExposed: in.MCPExposed}
	v, err := h.service.UpsertBinding(ctx, a, c.Param("workspace_id"), c.Param("toolset_id"), app)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": bindingView(v)})
}
func (h *Handler) deleteBinding(c *gin.Context) {
	configure(c)
	if !emptyMutation(c) {
		return
	}
	ctx, cancel, a, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	if err := h.service.DeleteBinding(ctx, a, c.Param("workspace_id"), c.Param("toolset_id"), c.Param("tool_version_id")); err != nil {
		fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
func (h *Handler) toolsetPreflight(c *gin.Context) {
	configure(c)
	if !emptyMutation(c) {
		return
	}
	ctx, cancel, a, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.ToolsetPreflight(ctx, a, c.Param("workspace_id"), c.Param("toolset_id"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": preflightView(v)})
}
func (h *Handler) publishToolset(c *gin.Context) {
	configure(c)
	if !emptyMutation(c) {
		return
	}
	ctx, cancel, a, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.PublishToolset(ctx, a, c.Param("workspace_id"), c.Param("toolset_id"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": toolsetView(v)})
}
func (h *Handler) retireToolset(c *gin.Context) {
	configure(c)
	if !emptyMutation(c) {
		return
	}
	ctx, cancel, a, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.RetireToolset(ctx, a, c.Param("workspace_id"), c.Param("toolset_id"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": toolsetView(v)})
}
