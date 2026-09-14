//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	connectionoauth "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/oauth"
	connectionpg "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/postgres"
	connectionsupplycredentials "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/supplycredentials"
	connectionapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
	filesecret "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/filesecret"
	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
	supplypublic "github.com/orz-i/mender/backend/internal/contexts/supply/public"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

type oauthRefreshRoundTrip struct {
	raceCalls atomic.Int32
	raceReady chan struct{}
	raceOnce  sync.Once
}

func (r *oauthRefreshRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.String() != "https://provider.example/oauth/token" || req.Method != http.MethodPost {
		return nil, errors.New("OAuth refresh escaped reviewed endpoint")
	}
	clientID, clientSecret, ok := req.BasicAuth()
	if !ok || clientID != "mender-refresh-client" || clientSecret != "mounted-refresh-client-secret" {
		return nil, errors.New("OAuth refresh used wrong client credentials")
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	form, err := url.ParseQuery(string(body))
	if err != nil || form.Get("grant_type") != "refresh_token" || len(form) != 2 {
		return nil, errors.New("OAuth refresh form is invalid")
	}
	refresh := form.Get("refresh_token")
	status := http.StatusOK
	payload := ""
	switch refresh {
	case "refresh-race-v1":
		call := r.raceCalls.Add(1)
		if call == 2 {
			r.raceOnce.Do(func() { close(r.raceReady) })
		}
		select {
		case <-r.raceReady:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		payload = fmt.Sprintf(`{"access_token":"access-race-v2-%d","token_type":"Bearer","expires_in":3600,"refresh_token":"refresh-race-v2-%d","scope":"resources.read profile.read"}`, call, call)
	case "refresh-keep-v1":
		payload = `{"access_token":"access-keep-v2","token_type":"Bearer","expires_in":3600}`
	case "refresh-scope-v1":
		payload = `{"access_token":"access-scope-v2","token_type":"Bearer","expires_in":3600,"scope":"resources.read"}`
	case "refresh-invalid-v1":
		status, payload = http.StatusBadRequest, `{"error":"invalid_grant"}`
	case "refresh-transient-v1":
		status, payload = http.StatusServiceUnavailable, `{"error":"temporarily_unavailable"}`
	case "refresh-revoke-v1":
		return nil, errors.New("revoked Connection unexpectedly reached token endpoint")
	default:
		return nil, fmt.Errorf("unexpected refresh secret %q", refresh)
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
}

func exerciseOAuthRefreshRuntime(t *testing.T, ctx context.Context, owner *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	refresherDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_oauth_refresh_", migrations.GrantOAuthRefresher, database.OAuthRefresherRole)
	managerDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_oauth_manager_", migrations.GrantConnectionManager, database.ConnectionManagerRole)

	transport := &oauthRefreshRoundTrip{raceReady: make(chan struct{})}
	provider, err := connectionoauth.New(connectionoauth.Config{
		ProviderID:       "provider_oauth_refresh",
		AuthorizationURL: "https://provider.example/oauth/authorize",
		TokenURL:         "https://provider.example/oauth/token",
		ClientID:         "mender-refresh-client",
		ClientSecret:     "mounted-refresh-client-secret",
		RedirectURL:      "https://mender.example/api/console/v1/connections/oauth/callback",
		Scopes:           []string{"resources.read", "profile.read"},
	}, &http.Client{Timeout: 3 * time.Second, Transport: transport})
	must(t, err)

	root := t.TempDir()
	vault, err := filesecret.New(root)
	must(t, err)
	store := connectionsupplycredentials.New(vault)
	runtime, closeRuntime, err := bootstrap.BuildOAuthRefreshRuntime(ctx, bootstrap.OAuthRefreshRuntimeConfig{DatabaseURL: refresherDSN, LeadTime: 5 * time.Minute}, provider, store)
	must(t, err)
	defer closeRuntime()

	base := time.Now().UTC().Truncate(time.Microsecond)
	_, err = owner.Exec(ctx, `INSERT INTO identity.workspaces(id,created_at) VALUES('ws_oauth_refresh',$1) ON CONFLICT DO NOTHING`, base.Add(-time.Hour))
	must(t, err)
	seed := func(label string, expiry time.Time) {
		t.Helper()
		connectionID := "conn_refresh_" + label
		accessRef := "oauth_access_" + label + "_v1"
		refreshRef := "oauth_refresh_" + label + "_v1"
		_, e := owner.Exec(ctx, `INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at)
		 VALUES('ws_oauth_refresh',$1,'provider_oauth_refresh',$2,'active',1,$3,$4)`, connectionID, accessRef, base.Add(-time.Hour), expiry)
		must(t, e)
		_, e = owner.Exec(ctx, `INSERT INTO connections.connection_grants(workspace_id,connection_id,subject_id,active,created_at,expires_at)
		 VALUES('ws_oauth_refresh',$1,'sa_oauth_refresh',true,$2,$3)`, connectionID, base.Add(-time.Hour), expiry)
		must(t, e)
		_, e = owner.Exec(ctx, `INSERT INTO connections.oauth_refresh_sessions(
		 workspace_id,connection_id,provider_id,refresh_credential_ref,refresh_secret_revision,connection_revision,required_scopes,granted_scopes,state,last_error_code,created_at,updated_at,last_refreshed_at)
		 VALUES('ws_oauth_refresh',$1,'provider_oauth_refresh',$2,1,1,ARRAY['resources.read','profile.read'],ARRAY['resources.read','profile.read'],'active',NULL,$3,$3,NULL)`, connectionID, refreshRef, base.Add(-time.Hour))
		must(t, e)
		must(t, vault.StoreCredential(ctx, supplyAddress("provider_oauth_refresh", connectionID, accessRef, 1), []byte("access-"+label+"-v1")))
		must(t, vault.StoreCredential(ctx, supplyAddress("provider_oauth_refresh", connectionID, refreshRef, 1), []byte("refresh-"+label+"-v1")))
	}

	seed("race", base.Add(time.Minute))
	type refreshResult struct {
		receipt connectionapp.OAuthRefreshReceipt
		err     error
	}
	results := make(chan refreshResult, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			receipt, e := runtime.RefreshOne(ctx, "ws_oauth_refresh")
			results <- refreshResult{receipt: receipt, err: e}
		}()
	}
	group.Wait()
	close(results)
	var winners, losers int
	for result := range results {
		switch {
		case result.err == nil && result.receipt.Outcome == "refreshed" && result.receipt.Revision == 2:
			winners++
		case errors.Is(result.err, connectionapp.ErrNoOAuthRefreshCandidate):
			losers++
		default:
			t.Fatal("unexpected concurrent refresh result", result.receipt, result.err)
		}
	}
	if winners != 1 || losers != 1 || transport.raceCalls.Load() != 2 {
		t.Fatal("OAuth refresh CAS was not single-winner", winners, losers, transport.raceCalls.Load())
	}
	var accessRef, refreshRef, state string
	var revision, refreshRevision int64
	must(t, owner.QueryRow(ctx, `SELECT c.credential_version_ref,c.revision,c.state,s.refresh_credential_ref,s.refresh_secret_revision
	 FROM connections.connections c JOIN connections.oauth_refresh_sessions s ON (s.workspace_id,s.connection_id)=(c.workspace_id,c.id)
	 WHERE c.workspace_id='ws_oauth_refresh' AND c.id='conn_refresh_race'`).Scan(&accessRef, &revision, &state, &refreshRef, &refreshRevision))
	if revision != 2 || refreshRevision != 2 || state != "active" || accessRef == "oauth_access_race_v1" || refreshRef == "oauth_refresh_race_v1" {
		t.Fatal("winning OAuth refresh did not rotate immutable refs", accessRef, revision, state, refreshRef, refreshRevision)
	}
	accessSecret, err := vault.ResolveSecret(ctx, supplyapp.SecretRequest{ProviderID: "provider_oauth_refresh", ConnectionID: "conn_refresh_race", CredentialVersionRef: accessRef, ConnectionRevision: revision})
	must(t, err)
	if !strings.HasPrefix(string(accessSecret.Bytes()), "access-race-v2-") {
		t.Fatal("runtime credential did not point at winning refreshed access token")
	}
	entries, err := os.ReadDir(root)
	must(t, err)
	if len(entries) != 2 {
		t.Fatal("OAuth refresh loser/old secret cleanup did not converge", len(entries))
	}

	seed("keep", base.Add(time.Minute))
	keepReceipt, err := runtime.RefreshOne(ctx, "ws_oauth_refresh")
	must(t, err)
	if keepReceipt.Outcome != "refreshed" || keepReceipt.Revision != 2 {
		t.Fatal("refresh without token rotation did not succeed", keepReceipt)
	}
	must(t, owner.QueryRow(ctx, `SELECT s.refresh_credential_ref,s.refresh_secret_revision FROM connections.oauth_refresh_sessions s WHERE workspace_id='ws_oauth_refresh' AND connection_id='conn_refresh_keep'`).Scan(&refreshRef, &refreshRevision))
	if refreshRef != "oauth_refresh_keep_v1" || refreshRevision != 1 {
		t.Fatal("provider omission unexpectedly rotated refresh secret", refreshRef, refreshRevision)
	}
	if raw, e := store.Load(ctx, connectionapp.OAuthCredentialAddress{ProviderID: "provider_oauth_refresh", ConnectionID: "conn_refresh_keep", CredentialVersionRef: refreshRef, ConnectionRevision: refreshRevision}); e != nil || string(raw) != "refresh-keep-v1" {
		t.Fatal("existing refresh secret was not retained", e)
	}

	seed("scope", base.Add(time.Minute))
	scopeReceipt, err := runtime.RefreshOne(ctx, "ws_oauth_refresh")
	must(t, err)
	if scopeReceipt.Outcome != "scope_reduced" || scopeReceipt.Revision != 2 {
		t.Fatal("scope shrink did not fail closed", scopeReceipt)
	}
	assertOAuthRefreshFailure(t, ctx, owner, "scope", "scope_reduced")

	seed("invalid", base.Add(time.Minute))
	invalidReceipt, err := runtime.RefreshOne(ctx, "ws_oauth_refresh")
	must(t, err)
	if invalidReceipt.Outcome != "invalid_grant" || invalidReceipt.Revision != 2 {
		t.Fatal("invalid_grant did not converge to durable error", invalidReceipt)
	}
	assertOAuthRefreshFailure(t, ctx, owner, "invalid", "invalid_grant")

	seed("transient", base.Add(time.Minute))
	if _, err = runtime.RefreshOne(ctx, "ws_oauth_refresh"); !errors.Is(err, connectionapp.ErrUnavailable) {
		t.Fatal("transient provider failure was not retryable", err)
	}
	must(t, owner.QueryRow(ctx, `SELECT c.state,c.revision,s.state FROM connections.connections c JOIN connections.oauth_refresh_sessions s ON (s.workspace_id,s.connection_id)=(c.workspace_id,c.id) WHERE c.workspace_id='ws_oauth_refresh' AND c.id='conn_refresh_transient'`).Scan(&state, &revision, &refreshRef))
	if state != "active" || revision != 1 || refreshRef != "active" {
		t.Fatal("transient OAuth refresh mutated durable state", state, revision, refreshRef)
	}
	farExpiry := base.Add(2 * time.Hour)
	_, err = owner.Exec(ctx, `UPDATE connections.connections SET expires_at=$1 WHERE workspace_id='ws_oauth_refresh' AND id='conn_refresh_transient'`, farExpiry)
	must(t, err)
	_, err = owner.Exec(ctx, `UPDATE connections.connection_grants SET expires_at=$1 WHERE workspace_id='ws_oauth_refresh' AND connection_id='conn_refresh_transient'`, farExpiry)
	must(t, err)

	seed("revoke", base.Add(time.Minute))
	managerPool, err := database.Open(ctx, managerDSN)
	must(t, err)
	defer managerPool.Close()
	revoked, err := connectionpg.New(managerPool).Revoke(ctx, "ws_oauth_refresh", "conn_refresh_revoke", base.Add(time.Second))
	must(t, err)
	if revoked.State != "revoked" || revoked.Revision != 2 {
		t.Fatal("Connection revoke did not close refresh eligibility", revoked)
	}
	must(t, owner.QueryRow(ctx, `SELECT state,connection_revision FROM connections.oauth_refresh_sessions WHERE workspace_id='ws_oauth_refresh' AND connection_id='conn_refresh_revoke'`).Scan(&state, &revision))
	if state != "revoked" || revision != 2 {
		t.Fatal("refresh sidecar did not follow Connection revoke", state, revision)
	}
	if _, err = runtime.RefreshOne(ctx, "ws_oauth_refresh"); !errors.Is(err, connectionapp.ErrNoOAuthRefreshCandidate) {
		t.Fatal("revoked/non-expiring Connections remained refresh candidates", err)
	}

	refresherPool, err := database.Open(ctx, refresherDSN)
	must(t, err)
	defer refresherPool.Close()
	for _, sql := range []string{
		`SELECT subject_id FROM connections.connection_grants`,
		`SELECT id FROM identity.api_keys`,
		`SELECT id FROM execution.runs`,
		`SELECT limit_micro FROM commerce.budget_periods`,
		`SELECT revision FROM supply.deployments`,
		`INSERT INTO connections.oauth_refresh_sessions(workspace_id,connection_id,provider_id,refresh_credential_ref,refresh_secret_revision,connection_revision,required_scopes,granted_scopes,state,created_at,updated_at) VALUES('ws_oauth_refresh','injected','provider_oauth_refresh','x',1,1,ARRAY['x'],ARRAY['x'],'active',now(),now())`,
	} {
		if _, e := refresherPool.Exec(ctx, sql); e == nil {
			t.Fatal("oauth-refresher role exceeded least privilege", sql)
		}
	}
	var databaseFacts string
	must(t, owner.QueryRow(ctx, `SELECT row_to_json(x)::text FROM (SELECT c.*,s.* FROM connections.connections c JOIN connections.oauth_refresh_sessions s ON (s.workspace_id,s.connection_id)=(c.workspace_id,c.id) WHERE c.workspace_id='ws_oauth_refresh' AND c.id='conn_refresh_race') x`).Scan(&databaseFacts))
	for _, forbidden := range []string{"access-race-v1", "access-race-v2-", "refresh-race-v1", "refresh-race-v2-", "mounted-refresh-client-secret"} {
		if strings.Contains(databaseFacts, forbidden) {
			t.Fatal("OAuth secret bytes leaked into PostgreSQL facts", forbidden)
		}
	}

	t.Log("real PostgreSQL OAuth refresh verified: reviewed token exchange, CAS single winner, immutable access/refresh secret rotation, no-rotation retention, scope/invalid_grant fail-closed, transient retry, revoke and least privilege")
}

func assertOAuthRefreshFailure(t *testing.T, ctx context.Context, owner *pgxpool.Pool, label, code string) {
	t.Helper()
	var connectionState, refreshState, errorCode string
	var revision int64
	must(t, owner.QueryRow(ctx, `SELECT c.state,c.revision,s.state,s.last_error_code FROM connections.connections c JOIN connections.oauth_refresh_sessions s ON (s.workspace_id,s.connection_id)=(c.workspace_id,c.id) WHERE c.workspace_id='ws_oauth_refresh' AND c.id=$1`, "conn_refresh_"+label).Scan(&connectionState, &revision, &refreshState, &errorCode))
	if connectionState != "error" || revision != 2 || refreshState != "error" || errorCode != code {
		t.Fatal("OAuth refresh permanent failure did not converge", label, connectionState, revision, refreshState, errorCode)
	}
}

func supplyAddress(providerID, connectionID, ref string, revision int64) supplypublic.CredentialAddress {
	return supplypublic.CredentialAddress{ProviderID: providerID, ConnectionID: connectionID, CredentialVersionRef: ref, ConnectionRevision: revision}
}
