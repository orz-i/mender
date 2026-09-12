package identity_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	identityhttp "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/inbound/httpapi"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/sessioncodec"
	identityapp "github.com/orz-i/mender/backend/internal/contexts/identity/application"
	identitydomain "github.com/orz-i/mender/backend/internal/contexts/identity/domain"
)

type humanRepo struct {
	mu       sync.Mutex
	sessions map[string]identitydomain.HumanSession
}

func (r *humanRepo) ResolveOIDCIdentity(context.Context, string, string) (identityapp.HumanIdentity, error) {
	return identityapp.HumanIdentity{UserID: "user_alpha", DisplayName: "Alpha"}, nil
}
func (r *humanRepo) CreateBrowserSession(_ context.Context, session identitydomain.HumanSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.Digest] = session
	return nil
}
func (r *humanRepo) FindBrowserSession(_ context.Context, digest string) (identitydomain.HumanSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.sessions[digest]
	if !ok {
		return identitydomain.HumanSession{}, identityapp.ErrNotFound
	}
	return value, nil
}
func (r *humanRepo) RevokeBrowserSession(_ context.Context, digest string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.sessions[digest]
	if !ok {
		return identityapp.ErrNotFound
	}
	value.RevokedAt = at
	r.sessions[digest] = value
	return nil
}
func (*humanRepo) ListWorkspaceMemberships(context.Context, string) ([]identitydomain.WorkspaceMembership, error) {
	return []identitydomain.WorkspaceMembership{{WorkspaceID: "ws_alpha", UserID: "user_alpha", Role: identitydomain.RoleAdmin, CreatedAt: time.Now().UTC()}}, nil
}
func (*humanRepo) FindWorkspaceMembership(context.Context, string, string) (identitydomain.WorkspaceMembership, error) {
	return identitydomain.WorkspaceMembership{WorkspaceID: "ws_alpha", UserID: "user_alpha", Role: identitydomain.RoleAdmin, CreatedAt: time.Now().UTC()}, nil
}

type humanClock struct{ at time.Time }

func (c humanClock) Now() time.Time { return c.at }

type fakeOIDC struct{}

func (fakeOIDC) AuthorizationURL(state, nonce, verifier string) string {
	return "https://idp.example/authorize?state=" + state + "&nonce=" + nonce + "&pkce=" + verifier
}
func (fakeOIDC) Exchange(_ context.Context, code, _, nonce string) (identityapp.VerifiedOIDCIdentity, error) {
	if code != "good-code" || nonce == "" {
		return identityapp.VerifiedOIDCIdentity{}, identityapp.ErrUnauthenticated
	}
	return identityapp.VerifiedOIDCIdentity{Issuer: "https://idp.example", Subject: "subject-alpha"}, nil
}

func TestHumanBrowserSessionOIDCAndCSRF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	clock := humanClock{at: time.Now().UTC().Truncate(time.Microsecond)}
	repo := &humanRepo{sessions: map[string]identitydomain.HumanSession{}}
	codec := sessioncodec.Codec{}
	sessions, err := identityapp.NewHumanSessionService(repo, codec, clock, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	login, err := identityapp.NewLoginService(fakeOIDC{}, sessions, codec, clock)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := identityhttp.NewFlowCookieCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := identityhttp.New(login, sessions, flow, false)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router)

	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if loginRecorder.Code != http.StatusSeeOther || !strings.HasPrefix(loginRecorder.Header().Get("Location"), "https://idp.example/authorize?") {
		t.Fatal("unexpected login redirect", loginRecorder.Code, loginRecorder.Header().Get("Location"))
	}
	var flowCookie *http.Cookie
	for _, cookie := range loginRecorder.Result().Cookies() {
		if cookie.Name == "mender_oidc_flow" {
			flowCookie = cookie
		}
	}
	if flowCookie == nil || !flowCookie.HttpOnly || flowCookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("unsafe OIDC flow cookie")
	}
	location := loginRecorder.Header().Get("Location")
	state := location[strings.Index(location, "state=")+6:]
	state = strings.Split(state, "&")[0]

	callback := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+state+"&code=good-code", nil)
	callback.AddCookie(flowCookie)
	callbackRecorder := httptest.NewRecorder()
	router.ServeHTTP(callbackRecorder, callback)
	if callbackRecorder.Code != http.StatusSeeOther || callbackRecorder.Header().Get("Location") != "/workspaces" {
		t.Fatal("callback failed", callbackRecorder.Code, callbackRecorder.Body.String())
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range callbackRecorder.Result().Cookies() {
		switch cookie.Name {
		case "mender_session":
			sessionCookie = cookie
		case "mender_csrf":
			csrfCookie = cookie
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || csrfCookie == nil || csrfCookie.HttpOnly {
		t.Fatal("browser session cookie contract invalid")
	}

	get := httptest.NewRequest(http.MethodGet, "/api/console/v1/session", nil)
	get.AddCookie(sessionCookie)
	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, get)
	if getRecorder.Code != 200 || !strings.Contains(getRecorder.Body.String(), "user_alpha") {
		t.Fatal("session read failed", getRecorder.Code, getRecorder.Body.String())
	}

	badLogout := httptest.NewRequest(http.MethodDelete, "/api/console/v1/session", nil)
	badLogout.AddCookie(sessionCookie)
	badLogout.AddCookie(csrfCookie)
	badLogoutRecorder := httptest.NewRecorder()
	router.ServeHTTP(badLogoutRecorder, badLogout)
	if badLogoutRecorder.Code != 403 {
		t.Fatal("logout without CSRF header was accepted", badLogoutRecorder.Code)
	}

	logout := httptest.NewRequest(http.MethodDelete, "/api/console/v1/session", nil)
	logout.AddCookie(sessionCookie)
	logout.AddCookie(csrfCookie)
	logout.Header.Set("X-Mender-CSRF", csrfCookie.Value)
	logoutRecorder := httptest.NewRecorder()
	router.ServeHTTP(logoutRecorder, logout)
	if logoutRecorder.Code != http.StatusNoContent {
		t.Fatal("logout failed", logoutRecorder.Code, logoutRecorder.Body.String())
	}
}
