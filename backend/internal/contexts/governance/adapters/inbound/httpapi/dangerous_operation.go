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

type DangerousOperationHandler struct {
	service *application.DangerousOperationService
	auth    application.Authorizer
}

type dangerousCommerceRequestInput struct {
	Action      string `json:"action"`
	BusinessKey string `json:"business_key"`
	BasisKind   string `json:"basis_kind"`
	BasisID     string `json:"basis_id"`
	Direction   string `json:"direction"`
	AmountMicro string `json:"amount_micro"`
	Currency    string `json:"currency"`
	Reason      string `json:"reason"`
	TTLSeconds  int64  `json:"ttl_seconds"`
}

func (h *DangerousOperationHandler) requestCommerce(c *gin.Context) {
	configure(c)
	workspace := c.Param("workspace_id")
	var input dangerousCommerceRequestInput
	if !validID(workspace) || decodeDangerous(c, &input) != nil || input.TTLSeconds < 60 || input.TTLSeconds > 1800 {
		fail(c, application.ErrInvalid)
		return
	}
	amount, err := strconv.ParseInt(input.AmountMicro, 10, 64)
	if err != nil || amount <= 0 || strconv.FormatInt(amount, 10) != input.AmountMicro {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := dangerousActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	item, err := h.service.RequestCommerceApproval(ctx, actor, workspace, application.CommerceApprovalRequest{
		Action: input.Action, BusinessKey: input.BusinessKey, BasisKind: input.BasisKind, BasisID: input.BasisID, Direction: input.Direction,
		AmountMicro: amount, Currency: input.Currency, Reason: input.Reason, TTL: time.Duration(input.TTLSeconds) * time.Second,
	})
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": dangerousView(item)})
}

func NewDangerousOperation(service *application.DangerousOperationService, auth application.Authorizer) (*DangerousOperationHandler, error) {
	if service == nil || auth == nil {
		return nil, application.ErrUnavailable
	}
	return &DangerousOperationHandler{service: service, auth: auth}, nil
}

func (h *DangerousOperationHandler) Register(router *gin.Engine) {
	base := "/api/admin/v1/workspaces/:workspace_id/dangerous-operations"
	router.GET(base, h.list)
	router.POST(base+"/release-emergency-requests", h.requestReleaseEmergency)
	router.POST(base+"/commerce-requests", h.requestCommerce)
	router.POST(base+"/:approval_id/approve", h.approve)
	router.POST(base+"/:approval_id/reject", h.reject)
}

func dangerousActor(c *gin.Context, auth application.Authorizer, mutation bool) (context.Context, context.CancelFunc, application.Actor, bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	raw, err := c.Cookie(sessionCookie)
	if err != nil {
		fail(c, application.ErrUnauthenticated)
		return ctx, cancel, application.Actor{}, false
	}
	var actor application.Actor
	if mutation {
		csrf := c.GetHeader("X-Mender-CSRF")
		if csrf == "" || len(csrf) > 256 {
			fail(c, application.ErrForbidden)
			return ctx, cancel, application.Actor{}, false
		}
		actor, err = auth.AuthenticateMutation(ctx, raw, csrf)
	} else {
		actor, err = auth.Authenticate(ctx, raw)
	}
	if err != nil {
		fail(c, err)
		return ctx, cancel, application.Actor{}, false
	}
	return ctx, cancel, actor, true
}

func dangerousView(v application.DangerousOperationApproval) gin.H {
	var amount any
	if v.AmountMicro != nil {
		amount = strconv.FormatInt(*v.AmountMicro, 10)
	}
	return gin.H{
		"id": v.ID, "requester_user_id": v.RequesterUserID, "subject_kind": v.SubjectKind, "subject_id": v.SubjectID,
		"action": v.Action, "target_kind": v.TargetKind, "target_id": v.TargetID, "target_version": v.TargetVersion,
		"parameters": json.RawMessage(v.ParametersJSON), "parameters_sha256": v.ParametersSHA256,
		"amount_micro": amount, "currency": nullableString(v.Currency), "reason": v.Reason, "state": v.State,
		"requested_at": v.RequestedAt, "expires_at": v.ExpiresAt, "reviewer_user_id": nullableString(v.ReviewerUserID),
		"reviewed_at": nullableTime(v.ReviewedAt), "decision_note": v.DecisionNote, "consumed_at": nullableTime(v.ConsumedAt),
	}
}

func decodeDangerous(c *gin.Context, dst any) error {
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 8192 {
		return application.ErrInvalid
	}
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 8193))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return application.ErrInvalid
	}
	return nil
}

func (h *DangerousOperationHandler) list(c *gin.Context) {
	configure(c)
	workspace := c.Param("workspace_id")
	if c.Request.URL.RawQuery != "" || !validID(workspace) {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := dangerousActor(c, h.auth, false)
	defer cancel()
	if !ok {
		return
	}
	items, err := h.service.List(ctx, actor, workspace)
	if err != nil {
		fail(c, err)
		return
	}
	data := make([]gin.H, 0, len(items))
	for _, item := range items {
		data = append(data, dangerousView(item))
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

type dangerousReleaseRequestInput struct {
	ReleaseID  string `json:"release_id"`
	Reason     string `json:"reason"`
	TTLSeconds int64  `json:"ttl_seconds"`
}

func (h *DangerousOperationHandler) requestReleaseEmergency(c *gin.Context) {
	configure(c)
	workspace := c.Param("workspace_id")
	var input dangerousReleaseRequestInput
	if !validID(workspace) || decodeDangerous(c, &input) != nil || input.TTLSeconds < 60 || input.TTLSeconds > 1800 {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := dangerousActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	item, err := h.service.RequestReleaseEmergency(ctx, actor, workspace, input.ReleaseID, input.Reason, time.Duration(input.TTLSeconds)*time.Second)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": dangerousView(item)})
}

func (h *DangerousOperationHandler) approve(c *gin.Context) { h.decide(c, true) }
func (h *DangerousOperationHandler) reject(c *gin.Context)  { h.decide(c, false) }

func (h *DangerousOperationHandler) decide(c *gin.Context, approve bool) {
	configure(c)
	workspace, id := c.Param("workspace_id"), c.Param("approval_id")
	var input decisionInput
	if !validID(workspace) || !validID(id) || decodeDangerous(c, &input) != nil {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := dangerousActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	var item application.DangerousOperationApproval
	var err error
	if approve {
		item, err = h.service.Approve(ctx, actor, workspace, id, input.Note)
	} else {
		item, err = h.service.Reject(ctx, actor, workspace, id, input.Note)
	}
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": dangerousView(item)})
}
