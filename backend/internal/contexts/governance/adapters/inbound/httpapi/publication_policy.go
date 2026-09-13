package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

type PublicationPolicyHandler struct {
	service *application.PolicyService
	auth    application.Authorizer
}

func NewPublicationPolicy(service *application.PolicyService, auth application.Authorizer) (*PublicationPolicyHandler, error) {
	if service == nil || auth == nil {
		return nil, application.ErrUnavailable
	}
	return &PublicationPolicyHandler{service: service, auth: auth}, nil
}

func (h *PublicationPolicyHandler) Register(router *gin.Engine) {
	base := "/api/admin/v1/workspaces/:workspace_id/publication-policy"
	router.GET(base, h.snapshot)
	router.POST(base+"/revisions", h.create)
	router.POST(base+"/revisions/:policy_id/activate", h.activate)
}

func policyRevisionView(v application.PolicyRevision) gin.H {
	return gin.H{"id": v.ID, "revision": strconv.FormatInt(v.Revision, 10), "state": v.State, "max_risk_level": v.MaxRiskLevel, "deny_unsafe_write": v.DenyUnsafeWrite, "deny_mcp_unsafe_write": v.DenyMCPUnsafeWrite, "created_by_user_id": nullableString(v.CreatedByUserID), "created_at": v.CreatedAt, "activated_by_user_id": nullableString(v.ActivatedByUserID), "activated_at": nullableTime(v.ActivatedAt), "retired_at": nullableTime(v.RetiredAt)}
}
func policyDecisionView(v application.PolicyDecision) gin.H {
	return gin.H{"sequence": strconv.FormatInt(v.Sequence, 10), "policy_revision_id": v.PolicyRevisionID, "policy_revision": strconv.FormatInt(v.PolicyRevision, 10), "target_kind": v.TargetKind, "target_id": v.TargetID, "target_revision": strconv.FormatInt(v.TargetRevision, 10), "risk_level": v.RiskLevel, "outcome": v.Outcome, "reason_codes": v.ReasonCodes, "evaluated_at": v.EvaluatedAt}
}

func (h *PublicationPolicyHandler) snapshot(c *gin.Context) {
	configure(c)
	if c.Request.URL.RawQuery != "" || !validID(c.Param("workspace_id")) {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := (&PublicationReviewHandler{auth: h.auth}).actor(c, false)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.Snapshot(ctx, actor, c.Param("workspace_id"))
	if err != nil {
		fail(c, err)
		return
	}
	revisions := make([]gin.H, 0, len(v.Revisions))
	for _, item := range v.Revisions {
		revisions = append(revisions, policyRevisionView(item))
	}
	decisions := make([]gin.H, 0, len(v.Decisions))
	for _, item := range v.Decisions {
		decisions = append(decisions, policyDecisionView(item))
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"revisions": revisions, "decisions": decisions}})
}

type policyInput struct {
	ID                 string `json:"id"`
	MaxRiskLevel       string `json:"max_risk_level"`
	DenyUnsafeWrite    bool   `json:"deny_unsafe_write"`
	DenyMCPUnsafeWrite bool   `json:"deny_mcp_unsafe_write"`
}

func decodePolicy(c *gin.Context) (policyInput, error) {
	var in policyInput
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

func (h *PublicationPolicyHandler) create(c *gin.Context) {
	configure(c)
	ws := c.Param("workspace_id")
	if !validID(ws) {
		fail(c, application.ErrInvalid)
		return
	}
	in, err := decodePolicy(c)
	if err != nil {
		fail(c, err)
		return
	}
	ctx, cancel, actor, ok := (&PublicationReviewHandler{auth: h.auth}).actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	item, err := h.service.Create(ctx, actor, ws, in.ID, in.MaxRiskLevel, in.DenyUnsafeWrite, in.DenyMCPUnsafeWrite)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": policyRevisionView(item)})
}

func (h *PublicationPolicyHandler) activate(c *gin.Context) {
	configure(c)
	ws, id := c.Param("workspace_id"), c.Param("policy_id")
	if !validID(ws) || !validID(id) || c.Request.URL.RawQuery != "" || c.Request.ContentLength > 0 {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := (&PublicationReviewHandler{auth: h.auth}).actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	item, err := h.service.Activate(ctx, actor, ws, id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": policyRevisionView(item)})
}
