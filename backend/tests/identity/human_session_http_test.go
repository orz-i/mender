package identity_test

import (
	"context"
	"encoding/json"
	"errors"
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

func TestHumanRunDelegationIsExplicitScopedAndRevocable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	clock := humanClock{at: time.Now().UTC().Truncate(time.Microsecond)}
	humans := &humanRepo{sessions: map[string]identitydomain.HumanSession{}}
	codec := sessioncodec.Codec{}
	sessions, err := identityapp.NewHumanSessionService(humans, codec, clock, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	issuedSession, err := sessions.IssueVerified(context.Background(), identityapp.VerifiedOIDCIdentity{Issuer: "https://idp.example", Subject: "subject-alpha"})
	if err != nil {
		t.Fatal(err)
	}
	repository := &delegationRepo{byID: map[string]identitydomain.RunDelegation{}, byDigest: map[string]string{}}
	delegations, err := identityapp.NewRunDelegationService(repository, sessions, codec, clock, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := identityhttp.NewRunDelegationHandler(sessions, delegations)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router)

	request := httptest.NewRequest(http.MethodPost, "/api/console/v1/workspaces/ws_alpha/run-delegations", strings.NewReader(`{"scopes":["run:read","run:cancel","run:input"]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Mender-CSRF", issuedSession.CSRFToken)
	request.AddCookie(&http.Cookie{Name: "mender_session", Value: issuedSession.SessionToken})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatal("delegation issue failed", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data struct {
			DelegationID string   `json:"delegation_id"`
			WorkspaceID  string   `json:"workspace_id"`
			Token        string   `json:"token"`
			Scopes       []string `json:"scopes"`
		} `json:"data"`
	}
	if err = json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Data.DelegationID == "" || response.Data.Token == "" || response.Data.WorkspaceID != "ws_alpha" {
		t.Fatal("invalid delegation response", err, recorder.Body.String())
	}
	principal, err := delegations.Authenticate(context.Background(), response.Data.Token)
	if err != nil {
		t.Fatal("delegation token did not authenticate", err)
	}
	if err = delegations.Authorize(context.Background(), principal, "ws_alpha", "run:read"); err != nil {
		t.Fatal("delegation lost read scope", err)
	}
	if err = delegations.Authorize(context.Background(), principal, "ws_alpha", "run:cancel"); err != nil {
		t.Fatal("delegation lost cancel scope", err)
	}
	if err = delegations.Authorize(context.Background(), principal, "ws_alpha", "run:input"); err != nil {
		t.Fatal("delegation lost supplemental-input scope", err)
	}
	if err = delegations.Authorize(context.Background(), principal, "ws_alpha", "run:create"); err == nil {
		t.Fatal("delegation unexpectedly gained run:create")
	}

	revoke := httptest.NewRequest(http.MethodDelete, "/api/console/v1/workspaces/ws_alpha/run-delegations/"+response.Data.DelegationID, nil)
	revoke.Header.Set("X-Mender-CSRF", issuedSession.CSRFToken)
	revoke.AddCookie(&http.Cookie{Name: "mender_session", Value: issuedSession.SessionToken})
	revokeRecorder := httptest.NewRecorder()
	router.ServeHTTP(revokeRecorder, revoke)
	if revokeRecorder.Code != http.StatusNoContent {
		t.Fatal("delegation revoke failed", revokeRecorder.Code, revokeRecorder.Body.String())
	}
	if _, err = delegations.Authenticate(context.Background(), response.Data.Token); !errors.Is(err, identityapp.ErrUnauthenticated) {
		t.Fatal("revoked delegation remained active", err)
	}
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

type delegationRepo struct {
	mu       sync.Mutex
	byID     map[string]identitydomain.RunDelegation
	byDigest map[string]string
}

func (r *delegationRepo) CreateRunDelegation(_ context.Context, d identitydomain.RunDelegation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[d.ID] = d
	r.byDigest[d.Digest] = d.ID
	return nil
}

func (r *delegationRepo) FindRunDelegationByDigest(_ context.Context, digest string) (identitydomain.RunDelegation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byDigest[digest]
	if !ok {
		return identitydomain.RunDelegation{}, identityapp.ErrNotFound
	}
	d := r.byID[id]
	d.MembershipRole = identitydomain.RoleAdmin
	return d, nil
}

func (r *delegationRepo) FindRunDelegationByID(_ context.Context, id string) (identitydomain.RunDelegation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.byID[id]
	if !ok {
		return identitydomain.RunDelegation{}, identityapp.ErrNotFound
	}
	d.MembershipRole = identitydomain.RoleAdmin
	return d, nil
}

func (r *delegationRepo) RevokeRunDelegation(_ context.Context, id, workspace, user string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.byID[id]
	if !ok || d.WorkspaceID != workspace || d.UserID != user || !d.RevokedAt.IsZero() {
		return identityapp.ErrNotFound
	}
	d.RevokedAt = at
	r.byID[id] = d
	return nil
}

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
	if sessionCookie == nil || !sessionCookie.HttpOnly || csrfCookie == nil || csrfCookie.HttpOnly || csrfCookie.Path != "/" {
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
