package connections_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	connectionhttp "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/inbound/httpapi"
	connectionapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
	connectiondomain "github.com/orz-i/mender/backend/internal/contexts/connections/domain"
)

type oauthClock struct{ at time.Time }

func (c oauthClock) Now() time.Time { return c.at }

type oauthRandom struct{ calls int }

func (r *oauthRandom) Token(bytes int) (string, error) {
	r.calls++
	switch bytes {
	case 32:
		if r.calls == 1 {
			return strings.Repeat("s", 43), nil
		}
		return strings.Repeat("v", 43), nil
	case 16:
		if r.calls == 3 {
			return strings.Repeat("c", 22), nil
		}
		return strings.Repeat("r", 22), nil
	default:
		return "", connectionapp.ErrUnavailable
	}
}

type oauthProvider struct {
	at        time.Time
	exchanges int
}

func (*oauthProvider) ProviderID() string { return "provider_alpha" }
func (*oauthProvider) AuthorizationURL(state, verifier string) (string, error) {
	return "https://provider.example/oauth/authorize?state=" + url.QueryEscape(state) + "&challenge=" + url.QueryEscape(verifier), nil
}
func (p *oauthProvider) Exchange(_ context.Context, code, verifier string) (connectionapp.OAuthAccessToken, error) {
	p.exchanges++
	if code != "good-code" || verifier != strings.Repeat("v", 43) {
		return connectionapp.OAuthAccessToken{}, connectionapp.ErrUnavailable
	}
	return connectionapp.OAuthAccessToken{Value: []byte("provider-access-token"), ExpiresAt: p.at.Add(time.Hour)}, nil
}

type oauthStore struct {
	address connectionapp.OAuthCredentialAddress
	secret  string
	deleted bool
}

func (s *oauthStore) Store(_ context.Context, address connectionapp.OAuthCredentialAddress, raw []byte) error {
	s.address, s.secret = address, string(raw)
	return nil
}
func (s *oauthStore) Delete(_ context.Context, address connectionapp.OAuthCredentialAddress) error {
	s.deleted = address == s.address
	return nil
}

type oauthRepo struct {
	input connectionapp.NewOAuthConnection
	fail  bool
}

func (r *oauthRepo) CreateOAuthConnection(_ context.Context, input connectionapp.NewOAuthConnection) (connectiondomain.Summary, error) {
	r.input = input
	if r.fail {
		return connectiondomain.Summary{}, connectionapp.ErrUnavailable
	}
	return connectiondomain.Summary{WorkspaceID: input.WorkspaceID, ConnectionID: input.ConnectionID, ProviderID: input.ProviderID, State: "active", Revision: input.Revision, CreatedAt: input.CreatedAt, ExpiresAt: input.ExpiresAt}, nil
}

func TestConnectionOAuthHTTPNeverReturnsProviderToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC().Truncate(time.Microsecond)
	auth := humanAuthorizer{manage: true}
	random := &oauthRandom{}
	provider := &oauthProvider{at: now}
	store := &oauthStore{}
	repository := &oauthRepo{}
	service, err := connectionapp.NewOAuth(auth, repository, provider, store, random, oauthClock{at: now}, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := connectionhttp.NewOAuthFlowCookieCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := connectionhttp.NewOAuth(service, auth, flow, false)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router)

	start := httptest.NewRequest(http.MethodPost, "/api/console/v1/workspaces/ws_alpha/connections/oauth/start", nil)
	start.AddCookie(&http.Cookie{Name: "mender_session", Value: "session-alpha"})
	start.Header.Set("X-Mender-CSRF", "csrf-alpha")
	started := httptest.NewRecorder()
	router.ServeHTTP(started, start)
	if started.Code != http.StatusOK || strings.Contains(started.Body.String(), "provider-access-token") {
		t.Fatal("unsafe OAuth start response", started.Code, started.Body.String())
	}
	var flowCookie *http.Cookie
	for _, cookie := range started.Result().Cookies() {
		if cookie.Name == "mender_connection_oauth_flow" {
			flowCookie = cookie
		}
	}
	if flowCookie == nil || !flowCookie.HttpOnly || flowCookie.Path != "/api/console/v1/connections/oauth/callback" {
		t.Fatal("OAuth flow cookie contract invalid")
	}
	challenge, err := flow.Decode(flowCookie.Value)
	if err != nil {
		t.Fatal(err)
	}

	callback := httptest.NewRequest(http.MethodGet, "/api/console/v1/connections/oauth/callback?state="+url.QueryEscape(challenge.State)+"&code=good-code", nil)
	callback.AddCookie(&http.Cookie{Name: "mender_session", Value: "session-alpha"})
	callback.AddCookie(flowCookie)
	completed := httptest.NewRecorder()
	router.ServeHTTP(completed, callback)
	if completed.Code != http.StatusSeeOther || completed.Header().Get("Location") != "/connections?workspace=ws_alpha&oauth=connected" {
		t.Fatal("OAuth callback failed", completed.Code, completed.Body.String(), completed.Header().Get("Location"))
	}
	if strings.Contains(completed.Body.String(), "provider-access-token") || strings.Contains(completed.Header().Get("Location"), "provider-access-token") {
		t.Fatal("provider access token leaked to browser")
	}
	if store.secret != "provider-access-token" || store.address.ProviderID != "provider_alpha" || repository.input.CredentialVersionRef == "" || repository.input.SubjectID != "user_alpha" {
		t.Fatal("OAuth credential was not persisted through reviewed server-side ports")
	}
}

func TestConnectionOAuthFailsClosedForViewerAndCompensatesRepositoryFailure(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	viewer, err := connectionapp.NewOAuth(humanAuthorizer{manage: false}, &oauthRepo{}, &oauthProvider{at: now}, &oauthStore{}, &oauthRandom{}, oauthClock{at: now}, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = viewer.Begin(context.Background(), connectionapp.HumanActor{UserID: "user_alpha"}, "ws_alpha"); err == nil {
		t.Fatal("viewer started Connection OAuth")
	}

	auth := humanAuthorizer{manage: true}
	store := &oauthStore{}
	random := &oauthRandom{}
	service, err := connectionapp.NewOAuth(auth, &oauthRepo{fail: true}, &oauthProvider{at: now}, store, random, oauthClock{at: now}, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := service.Begin(context.Background(), connectionapp.HumanActor{UserID: "user_alpha"}, "ws_alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Complete(context.Background(), connectionapp.HumanActor{UserID: "user_alpha"}, challenge, challenge.State, "good-code"); err == nil || !store.deleted {
		t.Fatal("repository failure did not compensate the stored provider credential", err, store.deleted)
	}
}
