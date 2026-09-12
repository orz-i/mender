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
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/connections/application"
)

const (
	connectionOAuthFlowCookie = "mender_connection_oauth_flow"
	connectionOAuthCallback   = "/api/console/v1/connections/oauth/callback"
)

type OAuthFlowCookieCodec struct{ key []byte }

func NewOAuthFlowCookieCodec(key []byte) (*OAuthFlowCookieCodec, error) {
	if len(key) != 32 {
		return nil, errors.New("Connection OAuth flow cookie requires a 32-byte signing key")
	}
	return &OAuthFlowCookieCodec{key: append([]byte(nil), key...)}, nil
}

func (c *OAuthFlowCookieCodec) signature(encoded string) []byte {
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write([]byte("mender-connection-oauth-flow-v1\x00"))
	_, _ = mac.Write([]byte(encoded))
	return mac.Sum(nil)
}

func (c *OAuthFlowCookieCodec) Encode(ch application.OAuthChallenge) (string, error) {
	payload, err := json.Marshal(struct {
		ProviderID, WorkspaceID, UserID, State, Verifier string
		ExpiresAt                                        time.Time
	}{ch.ProviderID, ch.WorkspaceID, ch.UserID, ch.State, ch.Verifier, ch.ExpiresAt})
	if err != nil || len(payload) > 3072 {
		return "", errors.New("Connection OAuth flow cookie unavailable")
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(c.signature(encoded)), nil
}

func (c *OAuthFlowCookieCodec) Decode(raw string) (application.OAuthChallenge, error) {
	if len(raw) < 1 || len(raw) > 6144 {
		return application.OAuthChallenge{}, application.ErrForbidden
	}
	var encoded, signature string
	for i := 0; i < len(raw); i++ {
		if raw[i] == '.' {
			if encoded != "" || i == 0 || i == len(raw)-1 {
				return application.OAuthChallenge{}, application.ErrForbidden
			}
			encoded, signature = raw[:i], raw[i+1:]
		}
	}
	if encoded == "" || signature == "" {
		return application.OAuthChallenge{}, application.ErrForbidden
	}
	provided, err := base64.RawURLEncoding.Strict().DecodeString(signature)
	if err != nil || subtle.ConstantTimeCompare(provided, c.signature(encoded)) != 1 {
		return application.OAuthChallenge{}, application.ErrForbidden
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return application.OAuthChallenge{}, application.ErrForbidden
	}
	var value struct {
		ProviderID, WorkspaceID, UserID, State, Verifier string
		ExpiresAt                                        time.Time
	}
	if err = json.Unmarshal(payload, &value); err != nil || !validID(value.ProviderID) || !validID(value.WorkspaceID) || !validID(value.UserID) || value.State == "" || value.Verifier == "" || value.ExpiresAt.IsZero() {
		return application.OAuthChallenge{}, application.ErrForbidden
	}
	return application.OAuthChallenge{ProviderID: value.ProviderID, WorkspaceID: value.WorkspaceID, UserID: value.UserID, State: value.State, Verifier: value.Verifier, ExpiresAt: value.ExpiresAt}, nil
}

type OAuthHandler struct {
	service    *application.OAuthService
	authorizer application.HumanAuthorizer
	flow       *OAuthFlowCookieCodec
	secure     bool
}

func NewOAuth(service *application.OAuthService, authorizer application.HumanAuthorizer, flow *OAuthFlowCookieCodec, secure bool) (*OAuthHandler, error) {
	if service == nil || authorizer == nil || flow == nil {
		return nil, errors.New("Connection OAuth HTTP adapter is not configured")
	}
	return &OAuthHandler{service: service, authorizer: authorizer, flow: flow, secure: secure}, nil
}

func (h *OAuthHandler) Register(router *gin.Engine) {
	router.POST("/api/console/v1/workspaces/:workspace_id/connections/oauth/start", h.start)
	router.GET(connectionOAuthCallback, h.callback)
}

func (h *OAuthHandler) actor(c *gin.Context, mutation bool) (context.Context, context.CancelFunc, application.HumanActor, bool) {
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

func (h *OAuthHandler) setFlowCookie(c *gin.Context, value string, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{Name: connectionOAuthFlowCookie, Value: value, Path: connectionOAuthCallback, Expires: expires, MaxAge: int(time.Until(expires).Seconds()), HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode})
}

func (h *OAuthHandler) clearFlowCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: connectionOAuthFlowCookie, Value: "", Path: connectionOAuthCallback, MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode})
}

func (h *OAuthHandler) start(c *gin.Context) {
	configure(c)
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 || !validID(c.Param("workspace_id")) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "OAuth start request is invalid."}})
		return
	}
	ctx, cancel, actor, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	challenge, err := h.service.Begin(ctx, actor, c.Param("workspace_id"))
	if err != nil {
		fail(c, err)
		return
	}
	encoded, err := h.flow.Encode(challenge)
	if err != nil {
		fail(c, application.ErrUnavailable)
		return
	}
	h.setFlowCookie(c, encoded, challenge.ExpiresAt)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"provider_id": challenge.ProviderID, "authorization_url": challenge.AuthorizationURL}})
}

func strictOAuthCallbackQuery(raw string) (code, state, providerError string, err error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "", "", "", err
	}
	for key, list := range values {
		if key != "code" && key != "state" && key != "error" && key != "error_description" || len(list) != 1 {
			return "", "", "", errors.New("unexpected OAuth callback query")
		}
	}
	state = values.Get("state")
	code = values.Get("code")
	providerError = values.Get("error")
	if state == "" || providerError == "" && code == "" || providerError != "" && code != "" {
		return "", "", "", errors.New("invalid OAuth callback query")
	}
	return code, state, providerError, nil
}

func (h *OAuthHandler) callback(c *gin.Context) {
	configure(c)
	code, state, providerError, err := strictOAuthCallbackQuery(c.Request.URL.RawQuery)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "OAuth callback is invalid."}})
		return
	}
	ctx, cancel, actor, ok := h.actor(c, false)
	defer cancel()
	if !ok {
		return
	}
	rawFlow, err := c.Cookie(connectionOAuthFlowCookie)
	if err != nil {
		fail(c, application.ErrForbidden)
		return
	}
	challenge, err := h.flow.Decode(rawFlow)
	if err != nil || challenge.State != state || challenge.UserID != actor.UserID {
		fail(c, application.ErrForbidden)
		return
	}
	h.clearFlowCookie(c)
	if providerError != "" {
		if err = h.authorizer.Authorize(ctx, actor, challenge.WorkspaceID, "connection:manage"); err != nil {
			fail(c, err)
			return
		}
		c.Redirect(http.StatusSeeOther, "/connections?workspace="+url.QueryEscape(challenge.WorkspaceID)+"&oauth=denied")
		return
	}
	if _, err = h.service.Complete(ctx, actor, challenge, state, code); err != nil {
		fail(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/connections?workspace="+url.QueryEscape(challenge.WorkspaceID)+"&oauth=connected")
}
