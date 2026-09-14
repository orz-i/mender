//go:build integration

package integration_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	commerceidentityaccess "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/outbound/identityaccess"
	commercepg "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/outbound/postgres"
	commerceapp "github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	connectionidentityaccess "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/identityaccess"
	connectionpg "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/postgres"
	connectionsupplycredentials "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/supplycredentials"
	connectionapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
	runidentityaccess "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/identityaccess"
	runports "github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	rundomain "github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	identityfacade "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/inbound/facade"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/sessioncodec"
	identityapp "github.com/orz-i/mender/backend/internal/contexts/identity/application"
	identitydomain "github.com/orz-i/mender/backend/internal/contexts/identity/domain"
	"github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/filesecret"
	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
	"github.com/orz-i/mender/backend/internal/platform/canonicaljson"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	admissionidentitystart "github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/identitystart"
	admissionapp "github.com/orz-i/mender/backend/internal/processes/admission/application"
	launchidentity "github.com/orz-i/mender/backend/internal/processes/consolelaunch/adapters/outbound/identityaccess"
	launchpg "github.com/orz-i/mender/backend/internal/processes/consolelaunch/adapters/outbound/postgres"
	launchapp "github.com/orz-i/mender/backend/internal/processes/consolelaunch/application"
	"github.com/orz-i/mender/backend/migrations"
)

type humanSessionClock struct{ at time.Time }

func (c *humanSessionClock) Now() time.Time { return c.at }

type integrationOAuthRandom struct{ calls int }

func (r *integrationOAuthRandom) Token(bytes int) (string, error) {
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

type integrationOAuthProvider struct{ at time.Time }

func (*integrationOAuthProvider) ProviderID() string { return "provider_human_oauth" }
func (*integrationOAuthProvider) AuthorizationURL(state, verifier string) (string, error) {
	if state == "" || verifier == "" {
		return "", connectionapp.ErrUnavailable
	}
	return "https://provider.example/oauth/authorize?state=" + state, nil
}
func (p *integrationOAuthProvider) Exchange(_ context.Context, code, verifier string) (connectionapp.OAuthAccessToken, error) {
	if code != "good-code" || verifier == "" {
		return connectionapp.OAuthAccessToken{}, connectionapp.ErrUnavailable
	}
	return connectionapp.OAuthAccessToken{Value: []byte("alpha-provider-credential"), ExpiresAt: p.at.Add(time.Hour)}, nil
}

func exerciseHumanBrowserSessions(t *testing.T, ctx context.Context, owner *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	sessionsPool, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_browser_session_", migrations.GrantBrowserSession)
	connectionPool, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_connection_manager_", migrations.GrantConnectionManager)
	observerPool, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_commerce_observer_", migrations.GrantCommerceObserver)
	launchPool, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_console_launch_", migrations.GrantRuntime)
	admissionPool, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_human_admission_", migrations.GrantAdmission)
	must(t, database.BrowserSessionRole(ctx, sessionsPool))
	must(t, database.ConnectionManagerRole(ctx, connectionPool))
	must(t, database.CommerceObserverRole(ctx, observerPool))
	must(t, database.RuntimeRole(ctx, launchPool))
	if database.BrowserSessionRole(ctx, owner) == nil {
		t.Fatal("owner accepted as browser-session principal")
	}
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	provision := identitypg.HumanProvision{UserID: "user_human_alpha", DisplayName: "Alpha User", Issuer: "https://issuer.example", Subject: "subject-human-alpha", WorkspaceID: "ws_human_alpha", Role: identitydomain.RoleAdmin, CreatedAt: base}
	must(t, identitypg.New(owner).ProvisionHuman(ctx, provision))
	must(t, identitypg.New(owner).ProvisionHuman(ctx, provision))
	ensurePermissiveExecutionPolicy(t, ctx, owner, "ws_human_alpha", base)
	conflict := provision
	conflict.UserID = "user_human_other"
	if err := identitypg.New(owner).ProvisionHuman(ctx, conflict); !errors.Is(err, identityapp.ErrForbidden) {
		t.Fatal("existing OIDC subject was rebound to another user", err)
	}

	clock := &humanSessionClock{at: base.Add(time.Minute)}
	service, err := identityapp.NewHumanSessionService(identitypg.NewHumanSessions(sessionsPool), sessioncodec.Codec{}, clock, 8*time.Hour)
	must(t, err)
	issued, err := service.IssueVerified(ctx, identityapp.VerifiedOIDCIdentity{Issuer: "https://issuer.example", Subject: "subject-human-alpha"})
	must(t, err)
	if issued.SessionToken == "" || issued.CSRFToken == "" || issued.User.UserID != "user_human_alpha" {
		t.Fatal("invalid issued human session", issued.User.UserID)
	}
	principal, err := service.Authenticate(ctx, issued.SessionToken)
	must(t, err)
	workspaces, err := service.ListWorkspaces(ctx, principal)
	must(t, err)
	if len(workspaces) != 1 || workspaces[0].WorkspaceID != "ws_human_alpha" || workspaces[0].Role != identitydomain.RoleAdmin {
		t.Fatal("workspace membership not revalidated", workspaces)
	}
	if err = service.AuthorizeWorkspace(ctx, principal, "ws_human_alpha", "connection:manage"); err != nil {
		t.Fatal("admin membership denied connection management", err)
	}
	_, err = owner.Exec(ctx, `INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at) VALUES('ws_human_alpha','conn_human_alpha','provider_human_alpha','secret_never_to_browser','active',1,$1,$2)`, base, base.Add(2*time.Hour))
	must(t, err)
	humanAccess := connectionidentityaccess.NewHuman(identityfacade.NewHuman(service))
	connectionService, err := connectionapp.NewHuman(connectionpg.New(connectionPool), humanAccess)
	must(t, err)
	actor, err := humanAccess.Authenticate(ctx, issued.SessionToken)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO catalog.tool_versions(id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable,state,published_at) VALUES('tool_human_launch_v1','tool_human_launch','1.0.0','provider_human_alpha','price_human_launch_v1','deploy_human_launch_v1','Human Search','Reviewed human launch option','{"type":"object","additionalProperties":false,"required":["query"],"properties":{"query":{"type":"string","minLength":1}}}'::jsonb,'{"type":"object"}'::jsonb,'read_only','safe_read',false,'published',$1)`, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO distribution.toolset_bindings(workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,connection_id,state,published_at) VALUES('ws_human_alpha','set_human_launch_v1','tool_human_launch','1.0.0','tool_human_launch_v1','budget_human_launch','conn_human_alpha','published',$1)`, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO commerce.price_versions(id,tool_version_id,currency,reserve_micro,charge_micro,billing_policy,starts_at,ends_at,active) VALUES('price_human_launch_v1','tool_human_launch_v1','USD',75,75,'fixed_success_only',$1,$2,true)`, base, base.Add(2*time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro) VALUES('ws_human_alpha','budget_human_launch','period_human_launch','USD',$1,$2,1000)`, base, base.Add(2*time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connection_grants(workspace_id,connection_id,subject_id,active,created_at,expires_at) VALUES('ws_human_alpha','conn_human_alpha','user_human_alpha',true,$1,$2)`, base, base.Add(2*time.Hour))
	must(t, err)
	launchAccess := launchidentity.New(identityfacade.NewHuman(service))
	launchService, err := launchapp.New(launchAccess, launchpg.New(launchPool), clock)
	must(t, err)
	launchActor, err := launchAccess.Authenticate(ctx, issued.SessionToken)
	must(t, err)
	launchOptions, err := launchService.List(ctx, launchActor, "ws_human_alpha")
	must(t, err)
	if len(launchOptions) != 1 || launchOptions[0].ToolsetVersionID != "set_human_launch_v1" || launchOptions[0].ConnectionID != "conn_human_alpha" || launchOptions[0].ReserveMicro != 75 || launchOptions[0].Currency != "USD" {
		t.Fatal("human launch discovery did not return the safe callable projection", launchOptions)
	}
	if _, err = launchService.List(ctx, launchActor, "ws_other"); !errors.Is(err, launchapp.ErrForbidden) {
		t.Fatal("human launch discovery crossed Workspace membership", err)
	}
	startDelegationService, err := identityapp.NewRunStartDelegationService(identitypg.NewRunStartDelegations(sessionsPool, sessionsPool), service, sessioncodec.Codec{}, clock, 5*time.Minute)
	must(t, err)
	startArguments := []byte(`{"query":"hello"}`)
	canonicalStartArguments, err := canonicaljson.Object(startArguments, 65536)
	must(t, err)
	startArgumentsHash := canonicaljson.SHA256(canonicalStartArguments)
	startDelegation, err := startDelegationService.Issue(ctx, principal, "ws_human_alpha", identityapp.RunStartConstraint{
		ToolsetVersionID: "set_human_launch_v1", ToolID: "tool_human_launch", ToolVersion: "1.0.0", ToolVersionID: "tool_human_launch_v1",
		ConnectionID: "conn_human_alpha", Currency: "USD", MaxChargeMicro: 75, IdempotencyKey: "human-start-alpha-0001", ArgumentsHash: startArgumentsHash,
	})
	must(t, err)
	if startDelegation.Token == "" || startDelegation.DelegationID == "" {
		t.Fatal("invalid Human StartRun delegation")
	}
	var storedStartDigest string
	must(t, owner.QueryRow(ctx, `SELECT digest FROM identity.run_start_delegations WHERE id=$1`, startDelegation.DelegationID).Scan(&storedStartDigest))
	if storedStartDigest == startDelegation.Token || len(storedStartDigest) != 64 {
		t.Fatal("raw Human StartRun token was persisted")
	}
	startAccess := admissionidentitystart.New(identityfacade.NewRunStartDelegations(startDelegationService))
	startCaller, err := startAccess.Authenticate(ctx, startDelegation.Token)
	must(t, err)
	if startCaller.Start == nil || startCaller.Start.ToolVersionID != "tool_human_launch_v1" || startCaller.CredentialID != startDelegation.DelegationID {
		t.Fatal("Human StartRun delegation did not project exact Admission capability", startCaller)
	}
	resolver, err := bootstrap.BuildAdmissionResolver(launchPool)
	must(t, err)
	humanAdmission, err := bootstrap.BuildAdmission(ctx, admissionPool, startAccess, resolver)
	must(t, err)
	receipt, err := humanAdmission.Admit(ctx, startCaller, admissionapp.Request{
		IdempotencyKey: "human-start-alpha-0001", ToolID: "tool_human_launch", ToolVersion: "1.0.0", ToolsetVersionID: "set_human_launch_v1",
		ConnectionID: "conn_human_alpha", Arguments: startArguments, Currency: "USD", MaxChargeMicro: "75",
	})
	must(t, err)
	if receipt.RunID == "" || receipt.Replayed {
		t.Fatal("Human StartRun was not admitted as a new durable Run", receipt)
	}
	var admittedSubject, admittedCredential, admittedToolVersion string
	var reservedMicro int64
	must(t, owner.QueryRow(ctx, `SELECT a.subject_id,a.credential_id,a.tool_version_id,r.amount_micro FROM execution.run_admissions a JOIN commerce.reservations r ON r.workspace_id=a.workspace_id AND r.id=a.reservation_id WHERE a.workspace_id='ws_human_alpha' AND a.run_id=$1`, receipt.RunID).Scan(&admittedSubject, &admittedCredential, &admittedToolVersion, &reservedMicro))
	if admittedSubject != "user_human_alpha" || admittedCredential != startDelegation.DelegationID || admittedToolVersion != "tool_human_launch_v1" || reservedMicro != 75 {
		t.Fatal("Human StartRun bypassed constrained Admission facts", admittedSubject, admittedCredential, admittedToolVersion, reservedMicro)
	}
	usageAccess := commerceidentityaccess.NewHuman(identityfacade.NewHuman(service))
	usageService, err := commerceapp.NewUsageService(commercepg.NewObservability(observerPool), usageAccess)
	must(t, err)
	usageActor, err := usageAccess.Authenticate(ctx, issued.SessionToken)
	must(t, err)
	usageSnapshot, err := usageService.Snapshot(ctx, usageActor, "ws_human_alpha")
	must(t, err)
	if len(usageSnapshot.BudgetPeriods) != 1 || usageSnapshot.BudgetPeriods[0].LimitMicro != 1000 || usageSnapshot.BudgetPeriods[0].ReservedMicro != 75 || usageSnapshot.BudgetPeriods[0].AvailableMicro() != 925 {
		t.Fatal("Human usage budget projection mismatch", usageSnapshot.BudgetPeriods)
	}
	if len(usageSnapshot.Entries) != 1 || usageSnapshot.Entries[0].RunID != string(receipt.RunID) || usageSnapshot.Entries[0].QuotaState != "held" || usageSnapshot.Entries[0].ReservedMicro != 75 {
		t.Fatal("Human usage entry projection mismatch", usageSnapshot.Entries)
	}
	if _, err = usageService.Snapshot(ctx, usageActor, "ws_other"); !errors.Is(err, commerceapp.ErrObservabilityForbidden) {
		t.Fatal("Human usage projection crossed Workspace membership", err)
	}
	if err = startAccess.Authorize(ctx, startCaller, admissionapp.Request{IdempotencyKey: "human-start-alpha-0001", ToolsetVersionID: "set_human_launch_v1", ToolID: "tool_human_launch", ToolVersion: "1.0.0", ConnectionID: "conn_human_alpha", Currency: "USD", MaxChargeMicro: "76"}); !errors.Is(err, admissionapp.ErrForbidden) {
		t.Fatal("Human StartRun delegation raised the approved charge cap", err)
	}
	if err = startAccess.Authorize(ctx, startCaller, admissionapp.Request{IdempotencyKey: "human-start-alpha-0002", ToolsetVersionID: "set_human_launch_v1", ToolID: "tool_human_launch", ToolVersion: "1.0.0", ConnectionID: "conn_human_alpha", Currency: "USD", MaxChargeMicro: "75"}); !errors.Is(err, admissionapp.ErrForbidden) {
		t.Fatal("Human StartRun delegation authorized a second logical Run", err)
	}
	vault, err := filesecret.New(t.TempDir())
	must(t, err)
	oauthService, err := connectionapp.NewOAuth(humanAccess, connectionpg.New(connectionPool), &integrationOAuthProvider{at: clock.at}, connectionsupplycredentials.New(vault), &integrationOAuthRandom{}, clock, 10*time.Minute)
	must(t, err)
	oauthChallenge, err := oauthService.Begin(ctx, actor, "ws_human_alpha")
	must(t, err)
	oauthCompleted, err := oauthService.Complete(ctx, actor, oauthChallenge, oauthChallenge.State, "good-code")
	must(t, err)
	if oauthCompleted.Connection.ProviderID != "provider_human_oauth" || oauthCompleted.Connection.State != "active" || oauthCompleted.Connection.Revision != 1 {
		t.Fatal("OAuth Connection did not converge", oauthCompleted.Connection)
	}
	var oauthCredentialRef, oauthGrantSubject string
	var oauthRevision int64
	must(t, owner.QueryRow(ctx, `SELECT credential_version_ref,revision FROM connections.connections WHERE workspace_id='ws_human_alpha' AND id=$1`, oauthCompleted.Connection.ConnectionID).Scan(&oauthCredentialRef, &oauthRevision))
	must(t, owner.QueryRow(ctx, `SELECT subject_id FROM connections.connection_grants WHERE workspace_id='ws_human_alpha' AND connection_id=$1`, oauthCompleted.Connection.ConnectionID).Scan(&oauthGrantSubject))
	if oauthGrantSubject != "user_human_alpha" || oauthCredentialRef == "" || oauthRevision != 1 {
		t.Fatal("OAuth Connection persisted incomplete identity facts", oauthGrantSubject, oauthCredentialRef, oauthRevision)
	}
	resolvedCredential, err := vault.ResolveSecret(ctx, supplyapp.SecretRequest{ProviderID: "provider_human_oauth", ConnectionID: oauthCompleted.Connection.ConnectionID, CredentialVersionRef: oauthCredentialRef, ConnectionRevision: oauthRevision})
	must(t, err)
	if string(resolvedCredential.Bytes()) != "alpha-provider-credential" {
		t.Fatal("OAuth provider credential was not stored behind the reviewed vault")
	}
	connections, err := connectionService.List(ctx, actor, "ws_human_alpha")
	must(t, err)
	if len(connections) != 2 {
		t.Fatal("safe Connection metadata not listed", connections)
	}
	foundOAuth := false
	for _, item := range connections {
		if item.ConnectionID == oauthCompleted.Connection.ConnectionID && item.ProviderID == "provider_human_oauth" && item.State == "active" {
			foundOAuth = true
		}
	}
	for _, sql := range []string{
		`SELECT id FROM commerce.price_versions LIMIT 1`,
		`SELECT reservation_id FROM commerce.usage_settlements LIMIT 1`,
		`UPDATE commerce.budget_periods SET limit_micro=limit_micro WHERE workspace_id='ws_human_alpha'`,
		`SELECT id FROM execution.runs LIMIT 1`,
	} {
		if _, err = observerPool.Exec(ctx, sql); err == nil {
			t.Fatal("commerce-observer role exceeded read-only safe projection", sql)
		}
	}
	if !foundOAuth {
		t.Fatal("OAuth Connection missing from safe metadata list", connections)
	}
	revoked, err := connectionService.Revoke(ctx, actor, "ws_human_alpha", "conn_human_alpha", clock.at.Add(time.Second))
	must(t, err)
	if revoked.State != "revoked" || revoked.Revision != 2 {
		t.Fatal("Connection revoke did not converge", revoked)
	}
	revokedAgain, err := connectionService.Revoke(ctx, actor, "ws_human_alpha", "conn_human_alpha", clock.at.Add(2*time.Second))
	must(t, err)
	if revokedAgain.Revision != 2 {
		t.Fatal("idempotent Connection revoke changed revision", revokedAgain.Revision)
	}
	delegationService, err := identityapp.NewRunDelegationService(identitypg.NewRunDelegations(sessionsPool, sessionsPool), service, sessioncodec.Codec{}, clock, 5*time.Minute)
	must(t, err)
	delegation, err := delegationService.Issue(ctx, principal, "ws_human_alpha", []string{"run:read", "run:cancel"})
	must(t, err)
	if delegation.Token == "" || delegation.DelegationID == "" || delegation.WorkspaceID != "ws_human_alpha" {
		t.Fatal("invalid Run delegation", delegation.DelegationID, delegation.WorkspaceID)
	}
	delegatedAccess := runidentityaccess.NewDelegated(identityfacade.NewRunDelegations(delegationService))
	delegatedCaller, err := delegatedAccess.Authenticate(ctx, delegation.Token)
	must(t, err)
	runCostAccess := commerceidentityaccess.NewDelegated(identityfacade.NewRunDelegations(delegationService))
	runCostService, err := commerceapp.NewRunCostService(commercepg.NewObservability(observerPool), runCostAccess)
	must(t, err)
	runCostActor, err := runCostAccess.AuthenticateRunCost(ctx, delegation.Token)
	must(t, err)
	runCost, err := runCostService.Get(ctx, runCostActor, "ws_human_alpha", string(receipt.RunID))
	must(t, err)
	if runCost.QuotaState != "held" || runCost.ReservedMicro != 75 || runCost.ChargedMicro != nil || runCost.ReleasedMicro() != 0 {
		t.Fatal("delegated Run cost did not expose the durable held reservation", runCost)
	}
	if _, err = runCostService.Get(ctx, runCostActor, "ws_other", string(receipt.RunID)); !errors.Is(err, commerceapp.ErrObservabilityForbidden) {
		t.Fatal("Run cost delegation crossed Workspace boundary", err)
	}
	probeRun := rundomain.RunID("run_delegated_probe")
	if err = delegatedAccess.Authorize(ctx, delegatedCaller, runports.ReadRun, probeRun); err != nil {
		t.Fatal("delegated read denied", err)
	}
	if err = delegatedAccess.Authorize(ctx, delegatedCaller, runports.CancelRun, probeRun); err != nil {
		t.Fatal("delegated cancel denied", err)
	}
	var storedDigest string
	must(t, owner.QueryRow(ctx, `SELECT digest FROM identity.run_delegations WHERE id=$1`, delegation.DelegationID).Scan(&storedDigest))
	if storedDigest == delegation.Token || len(storedDigest) != 64 {
		t.Fatal("raw delegation token was persisted")
	}
	_, err = owner.Exec(ctx, `INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at) VALUES('ws_human_alpha','conn_human_viewer','provider_human_alpha','secret_viewer','active',1,$1,$2)`, base, base.Add(2*time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `UPDATE identity.workspace_memberships SET role='viewer' WHERE workspace_id='ws_human_alpha' AND user_id='user_human_alpha'`)
	must(t, err)
	if err = startAccess.Authorize(ctx, startCaller, admissionapp.Request{IdempotencyKey: "human-start-alpha-0001", ToolsetVersionID: "set_human_launch_v1", ToolID: "tool_human_launch", ToolVersion: "1.0.0", ConnectionID: "conn_human_alpha", Currency: "USD", MaxChargeMicro: "75"}); !errors.Is(err, admissionapp.ErrForbidden) {
		t.Fatal("viewer retained Human StartRun authority", err)
	}
	if err = delegatedAccess.Authorize(ctx, delegatedCaller, runports.CancelRun, probeRun); !errors.Is(err, runports.ErrForbidden) {
		t.Fatal("viewer retained delegated cancel authority", err)
	}
	if err = delegatedAccess.Authorize(ctx, delegatedCaller, runports.ReadRun, probeRun); err != nil {
		t.Fatal("viewer lost delegated read authority", err)
	}
	if _, err = usageService.Snapshot(ctx, usageActor, "ws_human_alpha"); err != nil {
		t.Fatal("viewer lost read-only usage visibility", err)
	}
	if _, err = connectionService.Revoke(ctx, actor, "ws_human_alpha", "conn_human_viewer", clock.at.Add(3*time.Second)); !errors.Is(err, connectionapp.ErrForbidden) {
		t.Fatal("viewer membership revoked Connection", err)
	}
	must(t, delegationService.Revoke(ctx, principal, "ws_human_alpha", delegation.DelegationID))
	if _, err = delegatedAccess.Authenticate(ctx, delegation.Token); !errors.Is(err, runports.ErrUnauthenticated) {
		t.Fatal("revoked delegation remained active", err)
	}
	for _, sql := range []string{
		`SELECT credential_version_ref FROM connections.connections WHERE workspace_id='ws_human_alpha'`,
		`SELECT * FROM connections.connection_grants`,
		`SELECT id FROM identity.users`,
		`SELECT id FROM execution.runs`,
	} {
		if _, err = connectionPool.Exec(ctx, sql); err == nil {
			t.Fatal("connection-manager role exceeded safe metadata contract", sql)
		}
	}
	if err = service.Revoke(ctx, principal, "wrong-csrf"); !errors.Is(err, identityapp.ErrForbidden) {
		t.Fatal("wrong CSRF token accepted", err)
	}
	must(t, service.Revoke(ctx, principal, issued.CSRFToken))
	if _, err = service.Authenticate(ctx, issued.SessionToken); !errors.Is(err, identityapp.ErrUnauthenticated) {
		t.Fatal("revoked browser session remained active", err)
	}
	for _, sql := range []string{
		`SELECT digest FROM identity.api_keys LIMIT 1`,
		`SELECT id FROM execution.runs LIMIT 1`,
		`UPDATE identity.users SET disabled=true WHERE id='user_human_alpha'`,
		`UPDATE identity.workspace_memberships SET role='owner' WHERE user_id='user_human_alpha'`,
	} {
		if _, err = sessionsPool.Exec(ctx, sql); err == nil {
			t.Fatal("browser-session role exceeded identity contract", sql)
		}
	}
}
