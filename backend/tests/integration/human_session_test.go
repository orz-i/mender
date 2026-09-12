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
	_, err := owner.Exec(ctx, `INSERT INTO identity.workspaces(id,created_at) VALUES('ws_human_alpha',$1) ON CONFLICT DO NOTHING`, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.users(id,display_name,created_at) VALUES('user_human_alpha','Alpha User',$1)`, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.workspace_memberships(workspace_id,user_id,role,created_at) VALUES('ws_human_alpha','user_human_alpha','admin',$1)`, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.oidc_identities(issuer,subject,user_id,created_at) VALUES('https://issuer.example','subject-human-alpha','user_human_alpha',$1)`, base)
	must(t, err)

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
