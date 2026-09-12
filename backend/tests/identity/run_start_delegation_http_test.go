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

type startDelegationRepo struct {
	mu       sync.Mutex
	byID     map[string]identitydomain.RunStartDelegation
	byDigest map[string]string
}

func (r *startDelegationRepo) CreateRunStartDelegation(_ context.Context, d identitydomain.RunStartDelegation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[d.ID] = d
	r.byDigest[d.Digest] = d.ID
	return nil
}

func (r *startDelegationRepo) current(d identitydomain.RunStartDelegation) identitydomain.RunStartDelegation {
	d.MembershipRole = identitydomain.RoleAdmin
	return d
}

func (r *startDelegationRepo) FindRunStartDelegationByDigest(_ context.Context, digest string) (identitydomain.RunStartDelegation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byDigest[digest]
	if !ok {
		return identitydomain.RunStartDelegation{}, identityapp.ErrNotFound
	}
	return r.current(r.byID[id]), nil
}

func (r *startDelegationRepo) FindRunStartDelegationByID(_ context.Context, id string) (identitydomain.RunStartDelegation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.byID[id]
	if !ok {
		return identitydomain.RunStartDelegation{}, identityapp.ErrNotFound
	}
	return r.current(d), nil
}

func (r *startDelegationRepo) RevokeRunStartDelegation(_ context.Context, id, workspace, user string, at time.Time) error {
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

func TestHumanRunStartDelegationBindsExactLaunchAndChargeCap(t *testing.T) {
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
	repo := &startDelegationRepo{byID: map[string]identitydomain.RunStartDelegation{}, byDigest: map[string]string{}}
	delegations, err := identityapp.NewRunStartDelegationService(repo, sessions, codec, clock, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := identityhttp.NewRunStartDelegationHandler(sessions, delegations)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router)
	body := `{"toolset_version_id":"set_alpha_v1","tool_id":"tool_alpha","tool_version":"1.0.0","tool_version_id":"tool_alpha_v1","connection_id":"conn_alpha","currency":"USD","max_charge_micro":"75","idempotency_key":"human-alpha-0001"}`
	request := httptest.NewRequest(http.MethodPost, "/api/console/v1/workspaces/ws_alpha/run-start-delegations", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Mender-CSRF", issuedSession.CSRFToken)
	request.AddCookie(&http.Cookie{Name: "mender_session", Value: issuedSession.SessionToken})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, request)
	if w.Code != http.StatusCreated {
		t.Fatal("start delegation issue failed", w.Code, w.Body.String())
	}
	var response struct {
		Data struct {
			DelegationID   string `json:"delegation_id"`
			Token          string `json:"token"`
			MaxChargeMicro string `json:"max_charge_micro"`
		} `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Data.DelegationID == "" || response.Data.Token == "" || response.Data.MaxChargeMicro != "75" {
		t.Fatal("invalid start delegation response", err, w.Body.String())
	}
	principal, err := delegations.Authenticate(context.Background(), response.Data.Token)
	if err != nil {
		t.Fatal(err)
	}
	exact := identityapp.RunStartConstraint{ToolsetVersionID: "set_alpha_v1", ToolID: "tool_alpha", ToolVersion: "1.0.0", ToolVersionID: "tool_alpha_v1", ConnectionID: "conn_alpha", Currency: "USD", MaxChargeMicro: 75, IdempotencyKey: "human-alpha-0001"}
	if err = delegations.Authorize(context.Background(), principal, "ws_alpha", exact); err != nil {
		t.Fatal("exact delegated StartRun denied", err)
	}
	lower := exact
	lower.MaxChargeMicro = 70
	if err = delegations.Authorize(context.Background(), principal, "ws_alpha", lower); err != nil {
		t.Fatal("lower caller charge cap should stay within delegation", err)
	}
	for name, mutate := range map[string]func(*identityapp.RunStartConstraint){
		"tool":        func(v *identityapp.RunStartConstraint) { v.ToolID = "tool_other" },
		"toolversion": func(v *identityapp.RunStartConstraint) { v.ToolVersionID = "tool_other_v1" },
		"connection":  func(v *identityapp.RunStartConstraint) { v.ConnectionID = "conn_other" },
		"currency":    func(v *identityapp.RunStartConstraint) { v.Currency = "EUR" },
		"cap":         func(v *identityapp.RunStartConstraint) { v.MaxChargeMicro = 76 },
		"idempotency": func(v *identityapp.RunStartConstraint) { v.IdempotencyKey = "human-alpha-0002" },
	} {
		changed := exact
		mutate(&changed)
		if err = delegations.Authorize(context.Background(), principal, "ws_alpha", changed); !errors.Is(err, identityapp.ErrForbidden) {
			t.Fatal("delegation widened", name, err)
		}
	}
	revoke := httptest.NewRequest(http.MethodDelete, "/api/console/v1/workspaces/ws_alpha/run-start-delegations/"+response.Data.DelegationID, nil)
	revoke.Header.Set("X-Mender-CSRF", issuedSession.CSRFToken)
	revoke.AddCookie(&http.Cookie{Name: "mender_session", Value: issuedSession.SessionToken})
	revoked := httptest.NewRecorder()
	router.ServeHTTP(revoked, revoke)
	if revoked.Code != http.StatusNoContent {
		t.Fatal("start delegation revoke failed", revoked.Code, revoked.Body.String())
	}
	if _, err = delegations.Authenticate(context.Background(), response.Data.Token); !errors.Is(err, identityapp.ErrUnauthenticated) {
		t.Fatal("revoked start delegation remained active", err)
	}
}
