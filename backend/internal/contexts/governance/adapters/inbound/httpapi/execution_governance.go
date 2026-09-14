package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

type ExecutionGovernanceHandler struct {
	service *application.ExecutionGovernanceService
	auth    application.Authorizer
}

func NewExecutionGovernance(service *application.ExecutionGovernanceService, auth application.Authorizer) (*ExecutionGovernanceHandler, error) {
	if service == nil || auth == nil {
		return nil, application.ErrUnavailable
	}
	return &ExecutionGovernanceHandler{service: service, auth: auth}, nil
}

func (h *ExecutionGovernanceHandler) Register(router *gin.Engine) {
	base := "/api/admin/v1/workspaces/:workspace_id/execution-governance"
	router.GET(base, h.snapshot)
	router.POST(base+"/revisions", h.createPolicy)
	router.POST(base+"/revisions/:policy_id/activate", h.activatePolicy)
}

func executionGovernanceFilter(c *gin.Context) (application.ExecutionGovernanceFilter, error) {
	filter := application.ExecutionGovernanceFilter{Limit: 50}
	allowed := map[string]bool{
		"tool_version_id": true, "subject_kind": true, "risk_level": true, "outcome": true,
		"policy_revision": true, "confirmation_state": true, "before_decision_sequence": true,
		"before_confirmation_created_at": true, "before_confirmation_id": true, "limit": true,
	}
	values := c.Request.URL.Query()
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 {
			return filter, application.ErrInvalid
		}
	}
	filter.ToolVersionID = values.Get("tool_version_id")
	filter.SubjectKind = values.Get("subject_kind")
	filter.RiskLevel = values.Get("risk_level")
	filter.Outcome = values.Get("outcome")
	filter.ConfirmationState = values.Get("confirmation_state")
	if raw := values.Get("policy_revision"); raw != "" {
		value, ok := decimal(raw)
		if !ok {
			return filter, application.ErrInvalid
		}
		filter.PolicyRevision = value
	}
	if raw := values.Get("before_decision_sequence"); raw != "" {
		value, ok := decimal(raw)
		if !ok {
			return filter, application.ErrInvalid
		}
		filter.BeforeDecisionSequence = value
	}
	beforeCreated, beforeID := values.Get("before_confirmation_created_at"), values.Get("before_confirmation_id")
	if (beforeCreated == "") != (beforeID == "") {
		return filter, application.ErrInvalid
	}
	if beforeCreated != "" {
		parsed, err := time.Parse(time.RFC3339Nano, beforeCreated)
		if err != nil || !validID(beforeID) {
			return filter, application.ErrInvalid
		}
		filter.BeforeConfirmationCreatedAt = parsed.UTC()
		filter.BeforeConfirmationID = beforeID
	}
	if raw := values.Get("limit"); raw != "" {
		value, ok := decimal(raw)
		if !ok || value > 100 {
			return filter, application.ErrInvalid
		}
		filter.Limit = int(value)
	}
	return filter, nil
}

func executionGovernancePolicyView(v application.ExecutionGovernancePolicyRevision) gin.H {
	return gin.H{
		"id": v.ID, "revision": strconv.FormatInt(v.Revision, 10), "state": v.State,
		"max_unconfirmed_risk_level": v.MaxUnconfirmedRiskLevel, "max_machine_risk_level": v.MaxMachineRiskLevel,
		"deny_unsafe_write": v.DenyUnsafeWrite, "confirmation_ttl_seconds": v.ConfirmationTTLSeconds,
		"created_by_user_id": nullableString(v.CreatedByUserID), "created_at": v.CreatedAt,
		"activated_by_user_id": nullableString(v.ActivatedByUserID), "activated_at": nullableTime(v.ActivatedAt), "retired_at": nullableTime(v.RetiredAt),
	}
}

func executionGovernanceDecisionView(v application.ExecutionRiskDecision) gin.H {
	return gin.H{
		"sequence": strconv.FormatInt(v.Sequence, 10), "policy_revision_id": v.PolicyRevisionID,
		"policy_revision": strconv.FormatInt(v.PolicyRevision, 10), "subject_kind": v.SubjectKind, "subject_id": v.SubjectID,
		"toolset_version_id": v.ToolsetVersionID, "tool_version_id": v.ToolVersionID, "connection_id": v.ConnectionID,
		"arguments_hash": v.ArgumentsHash, "idempotency_key_hash": v.IdempotencyKeyHash,
		"risk_level": v.RiskLevel, "outcome": v.Outcome, "reason_codes": v.ReasonCodes, "evaluated_at": v.EvaluatedAt,
	}
}

func executionGovernanceConfirmationView(v application.ExecutionGovernanceConfirmation) gin.H {
	return gin.H{
		"id": v.ID, "user_id": v.UserID, "policy_revision_id": v.PolicyRevisionID,
		"policy_revision": strconv.FormatInt(v.PolicyRevision, 10), "toolset_version_id": v.ToolsetVersionID,
		"tool_version_id": v.ToolVersionID, "connection_id": v.ConnectionID, "arguments_hash": v.ArgumentsHash,
		"idempotency_key_hash": v.IdempotencyKeyHash, "risk_level": v.RiskLevel, "state": v.EffectiveState,
		"persisted_state": v.State, "created_at": v.CreatedAt, "expires_at": v.ExpiresAt,
		"consumed_at": nullableTime(v.ConsumedAt), "expired_at": nullableTime(v.ExpiredAt),
	}
}

func (h *ExecutionGovernanceHandler) snapshot(c *gin.Context) {
	configure(c)
	workspace := c.Param("workspace_id")
	if !validID(workspace) {
		fail(c, application.ErrInvalid)
		return
	}
	filter, err := executionGovernanceFilter(c)
	if err != nil {
		fail(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	raw, err := c.Cookie(sessionCookie)
	if err != nil {
		fail(c, application.ErrUnauthenticated)
		return
	}
	actor, err := h.auth.Authenticate(ctx, raw)
	if err != nil {
		fail(c, err)
		return
	}
	snapshot, err := h.service.Snapshot(ctx, actor, workspace, filter)
	if err != nil {
		fail(c, err)
		return
	}

	revisions := make([]gin.H, 0, len(snapshot.Revisions))
	var active any
	for _, item := range snapshot.Revisions {
		view := executionGovernancePolicyView(item)
		revisions = append(revisions, view)
		if item.State == "active" {
			active = view
		}
	}
	decisions := make([]gin.H, 0, len(snapshot.Decisions))
	for _, item := range snapshot.Decisions {
		decisions = append(decisions, executionGovernanceDecisionView(item))
	}
	confirmations := make([]gin.H, 0, len(snapshot.Confirmations))
	for _, item := range snapshot.Confirmations {
		confirmations = append(confirmations, executionGovernanceConfirmationView(item))
	}
	var nextDecision any
	if snapshot.NextBeforeDecisionSequence > 0 {
		nextDecision = strconv.FormatInt(snapshot.NextBeforeDecisionSequence, 10)
	}
	var nextConfirmation any
	if !snapshot.NextBeforeConfirmationCreatedAt.IsZero() {
		nextConfirmation = gin.H{"created_at": snapshot.NextBeforeConfirmationCreatedAt, "id": snapshot.NextBeforeConfirmationID}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"active_policy": active, "revisions": revisions, "decisions": decisions, "confirmations": confirmations,
		"next_before_decision_sequence": nextDecision, "next_confirmation_cursor": nextConfirmation,
	}})
}

type executionPolicyInput struct {
	ID                      string `json:"id"`
	MaxUnconfirmedRiskLevel string `json:"max_unconfirmed_risk_level"`
	MaxMachineRiskLevel     string `json:"max_machine_risk_level"`
	DenyUnsafeWrite         bool   `json:"deny_unsafe_write"`
	ConfirmationTTLSeconds  int    `json:"confirmation_ttl_seconds"`
}

func decodeExecutionPolicy(c *gin.Context) (executionPolicyInput, error) {
	var in executionPolicyInput
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 4096 {
		return in, application.ErrInvalid
	}
	dec := json.NewDecoder(io.LimitReader(c.Request.Body, 4097))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return in, application.ErrInvalid
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return in, application.ErrInvalid
	}
	return in, nil
}

func (h *ExecutionGovernanceHandler) createPolicy(c *gin.Context) {
	configure(c)
	workspace := c.Param("workspace_id")
	if !validID(workspace) {
		fail(c, application.ErrInvalid)
		return
	}
	in, err := decodeExecutionPolicy(c)
	if err != nil {
		fail(c, err)
		return
	}
	ctx, cancel, actor, ok := (&PublicationReviewHandler{auth: h.auth}).actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	item, err := h.service.CreatePolicy(ctx, actor, workspace, in.ID, in.MaxUnconfirmedRiskLevel, in.MaxMachineRiskLevel, in.DenyUnsafeWrite, in.ConfirmationTTLSeconds)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": executionGovernancePolicyView(item)})
}

func (h *ExecutionGovernanceHandler) activatePolicy(c *gin.Context) {
	configure(c)
	workspace, id := c.Param("workspace_id"), c.Param("policy_id")
	if !validID(workspace) || !validID(id) || c.Request.URL.RawQuery != "" || c.Request.ContentLength > 0 {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := (&PublicationReviewHandler{auth: h.auth}).actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	item, err := h.service.ActivatePolicy(ctx, actor, workspace, id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": executionGovernancePolicyView(item)})
}
