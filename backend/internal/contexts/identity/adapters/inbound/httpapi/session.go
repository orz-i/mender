package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/identity/application"
)

const (
	sessionCookie = "mender_session"
	csrfCookie    = "mender_csrf"
	flowCookie    = "mender_oidc_flow"
)

type FlowCookieCodec struct{ key []byte }

func NewFlowCookieCodec(key []byte) (*FlowCookieCodec, error) {
	if len(key) != 32 {
		return nil, errors.New("OIDC flow cookie requires a 32-byte signing key")
	}
	return &FlowCookieCodec{key: append([]byte(nil), key...)}, nil
}

func (c *FlowCookieCodec) Encode(ch application.LoginChallenge) (string, error) {
	payload, err := json.Marshal(struct {
		State, Nonce, Verifier string
		ExpiresAt              time.Time
	}{ch.State, ch.Nonce, ch.Verifier, ch.ExpiresAt})
	if err != nil || len(payload) > 2048 {
		return "", errors.New("OIDC flow cookie unavailable")
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + signature, nil
}

func (c *FlowCookieCodec) Decode(raw string) (application.LoginChallenge, error) {
	if len(raw) > 4096 {
		return application.LoginChallenge{}, application.ErrUnauthenticated
	}
	var encoded, signature string
	for i := 0; i < len(raw); i++ {
		if raw[i] == '.' {
			if encoded != "" || i == 0 || i == len(raw)-1 {
				return application.LoginChallenge{}, application.ErrUnauthenticated
			}
			encoded, signature = raw[:i], raw[i+1:]
		}
	}
	if encoded == "" || signature == "" {
		return application.LoginChallenge{}, application.ErrUnauthenticated
	}
	provided, err := base64.RawURLEncoding.Strict().DecodeString(signature)
	if err != nil {
		return application.LoginChallenge{}, application.ErrUnauthenticated
	}
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write([]byte(encoded))
	if subtle.ConstantTimeCompare(provided, mac.Sum(nil)) != 1 {
		return application.LoginChallenge{}, application.ErrUnauthenticated
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return application.LoginChallenge{}, application.ErrUnauthenticated
	}
	var value struct {
		State, Nonce, Verifier string
		ExpiresAt              time.Time
	}
	if err = json.Unmarshal(payload, &value); err != nil || value.State == "" || value.Nonce == "" || value.Verifier == "" || value.ExpiresAt.IsZero() {
		return application.LoginChallenge{}, application.ErrUnauthenticated
	}
	return application.LoginChallenge{State: value.State, Nonce: value.Nonce, Verifier: value.Verifier, ExpiresAt: value.ExpiresAt}, nil
}

type SessionHandler struct {
	login    *application.LoginService
	sessions *application.HumanSessionService
	flow     *FlowCookieCodec
	secure   bool
}

func New(login *application.LoginService, sessions *application.HumanSessionService, flow *FlowCookieCodec, secure bool) (*SessionHandler, error) {
	if login == nil || sessions == nil || flow == nil {
		return nil, errors.New("browser session HTTP adapter is not configured")
	}
	return &SessionHandler{login: login, sessions: sessions, flow: flow, secure: secure}, nil
}

func (h *SessionHandler) Register(router *gin.Engine) {
	router.GET("/auth/login", h.beginLogin)
	router.GET("/auth/callback", h.completeLogin)
	router.GET("/api/console/v1/session", h.getSession)
	router.DELETE("/api/console/v1/session", h.logout)
	router.GET("/api/console/v1/workspaces", h.listWorkspaces)
}

func (h *SessionHandler) setCookie(c *gin.Context, cookie *http.Cookie) {
	cookie.Secure = h.secure
	cookie.SameSite = http.SameSiteLaxMode
	http.SetCookie(c.Writer, cookie)
}

func (h *SessionHandler) clearCookie(c *gin.Context, name, path string, httpOnly bool) {
	h.setCookie(c, &http.Cookie{Name: name, Value: "", Path: path, HttpOnly: httpOnly, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
}

func (h *SessionHandler) beginLogin(c *gin.Context) {
	noStore(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	challenge, err := h.login.Begin(ctx)
	if err != nil {
		c.JSON(503, gin.H{"error": gin.H{"code": "OIDC_UNAVAILABLE", "message": "Login is temporarily unavailable."}})
		return
	}
	encoded, err := h.flow.Encode(challenge)
	if err != nil {
		c.JSON(503, gin.H{"error": gin.H{"code": "OIDC_UNAVAILABLE", "message": "Login is temporarily unavailable."}})
		return
	}
	h.setCookie(c, &http.Cookie{Name: flowCookie, Value: encoded, Path: "/auth/callback", HttpOnly: true, Expires: challenge.ExpiresAt, MaxAge: 600})
	c.Redirect(http.StatusSeeOther, challenge.AuthorizationURL)
}

func (h *SessionHandler) completeLogin(c *gin.Context) {
	noStore(c)
	if c.Request.URL.Query().Get("error") != "" {
		h.clearCookie(c, flowCookie, "/auth/callback", true)
		c.JSON(401, gin.H{"error": gin.H{"code": "OIDC_LOGIN_FAILED", "message": "Login was not completed."}})
		return
	}
	state, code := c.Query("state"), c.Query("code")
	flow, err := c.Cookie(flowCookie)
	if err != nil || state == "" || code == "" || len(state) > 256 || len(code) > 4096 {
		c.JSON(401, gin.H{"error": gin.H{"code": "OIDC_LOGIN_FAILED", "message": "Login was not completed."}})
		return
	}
	challenge, err := h.flow.Decode(flow)
	if err != nil {
		c.JSON(401, gin.H{"error": gin.H{"code": "OIDC_LOGIN_FAILED", "message": "Login was not completed."}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	issued, err := h.login.Complete(ctx, challenge, state, code)
	h.clearCookie(c, flowCookie, "/auth/callback", true)
	if err != nil {
		c.JSON(401, gin.H{"error": gin.H{"code": "OIDC_LOGIN_FAILED", "message": "Login was not completed."}})
		return
	}
	maxAge := int(time.Until(issued.ExpiresAt).Seconds())
	if maxAge < 1 {
		c.JSON(503, gin.H{"error": gin.H{"code": "SESSION_UNAVAILABLE", "message": "Session could not be created."}})
		return
	}
	h.setCookie(c, &http.Cookie{Name: sessionCookie, Value: issued.SessionToken, Path: "/", HttpOnly: true, Expires: issued.ExpiresAt, MaxAge: maxAge})
	h.setCookie(c, &http.Cookie{Name: csrfCookie, Value: issued.CSRFToken, Path: "/", HttpOnly: false, Expires: issued.ExpiresAt, MaxAge: maxAge})
	c.Redirect(http.StatusSeeOther, "/workspaces")
}

func (h *SessionHandler) authenticate(c *gin.Context) (application.HumanPrincipal, bool) {
	raw, err := c.Cookie(sessionCookie)
	if err != nil {
		c.JSON(401, gin.H{"error": gin.H{"code": "UNAUTHENTICATED", "message": "Login is required."}})
		return application.HumanPrincipal{}, false
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	principal, err := h.sessions.Authenticate(ctx, raw)
	if err != nil {
		c.JSON(401, gin.H{"error": gin.H{"code": "UNAUTHENTICATED", "message": "Login is required."}})
		return application.HumanPrincipal{}, false
	}
	return principal, true
}

func (h *SessionHandler) getSession(c *gin.Context) {
	noStore(c)
	principal, ok := h.authenticate(c)
	if !ok {
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"user_id": principal.UserID}})
}

func (h *SessionHandler) listWorkspaces(c *gin.Context) {
	noStore(c)
	principal, ok := h.authenticate(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	items, err := h.sessions.ListWorkspaces(ctx, principal)
	if err != nil {
		c.JSON(503, gin.H{"error": gin.H{"code": "IDENTITY_UNAVAILABLE", "message": "Workspace access is temporarily unavailable."}})
		return
	}
	type workspaceDTO struct {
		WorkspaceID string `json:"workspace_id"`
		Role        string `json:"role"`
	}
	result := make([]workspaceDTO, 0, len(items))
	for _, item := range items {
		result = append(result, workspaceDTO{WorkspaceID: item.WorkspaceID, Role: string(item.Role)})
	}
	c.JSON(200, gin.H{"data": result})
}

func (h *SessionHandler) logout(c *gin.Context) {
	noStore(c)
	principal, ok := h.authenticate(c)
	if !ok {
		return
	}
	csrfCookieValue, cookieErr := c.Cookie(csrfCookie)
	csrfHeader := c.GetHeader("X-Mender-CSRF")
	if cookieErr != nil || csrfHeader == "" || len(csrfHeader) != len(csrfCookieValue) || subtle.ConstantTimeCompare([]byte(csrfHeader), []byte(csrfCookieValue)) != 1 {
		c.JSON(403, gin.H{"error": gin.H{"code": "CSRF_REJECTED", "message": "Request could not be verified."}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if err := h.sessions.Revoke(ctx, principal, csrfHeader); err != nil {
		c.JSON(403, gin.H{"error": gin.H{"code": "CSRF_REJECTED", "message": "Request could not be verified."}})
		return
	}
	h.clearCookie(c, sessionCookie, "/", true)
	h.clearCookie(c, csrfCookie, "/", false)
	c.Status(http.StatusNoContent)
}
