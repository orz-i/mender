//go:build integration

package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/sessioncodec"
	identityapp "github.com/orz-i/mender/backend/internal/contexts/identity/application"
	identitydomain "github.com/orz-i/mender/backend/internal/contexts/identity/domain"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

type humanSessionClock struct{ at time.Time }

func (c *humanSessionClock) Now() time.Time { return c.at }

func exerciseHumanBrowserSessions(t *testing.T, ctx context.Context, owner *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	sessionsPool, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_browser_session_", migrations.GrantBrowserSession)
	must(t, database.BrowserSessionRole(ctx, sessionsPool))
	if database.BrowserSessionRole(ctx, owner) == nil {
		t.Fatal("owner accepted as browser-session principal")
	}
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	provision := identitypg.HumanProvision{UserID: "user_human_alpha", DisplayName: "Alpha User", Issuer: "https://issuer.example", Subject: "subject-human-alpha", WorkspaceID: "ws_human_alpha", Role: identitydomain.RoleAdmin, CreatedAt: base}
	must(t, identitypg.New(owner).ProvisionHuman(ctx, provision))
	must(t, identitypg.New(owner).ProvisionHuman(ctx, provision))
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
