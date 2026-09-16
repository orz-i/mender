//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	commercehttp "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/inbound/httpapi"
	commerceidentity "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/outbound/identityaccess"
	commercepg "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/outbound/postgres"
	commercerandom "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/outbound/random"
	commerceapp "github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	governancehttp "github.com/orz-i/mender/backend/internal/contexts/governance/adapters/inbound/httpapi"
	governanceidentity "github.com/orz-i/mender/backend/internal/contexts/governance/adapters/outbound/identityaccess"
	governancepg "github.com/orz-i/mender/backend/internal/contexts/governance/adapters/outbound/postgres"
	governancerandom "github.com/orz-i/mender/backend/internal/contexts/governance/adapters/outbound/random"
	governanceapp "github.com/orz-i/mender/backend/internal/contexts/governance/application"
	identityfacade "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/inbound/facade"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/sessioncodec"
	identityapp "github.com/orz-i/mender/backend/internal/contexts/identity/application"
	identitydomain "github.com/orz-i/mender/backend/internal/contexts/identity/domain"
	supplyhttp "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/inbound/httpapi"
	supplyidentity "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/identityaccess"
	supplyhash "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/manifesthash"
	supplypg "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/postgres"
	supplyrandom "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/random"
	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
	"github.com/orz-i/mender/backend/migrations"
)

type s4BrowserClock struct{}

func (s4BrowserClock) Now() time.Time { return time.Now().UTC() }

// The ordinary integration suite does not silently pretend to execute a browser.
// --browser explicitly requires built frontends + project-local Chromium. Sessions
// are issued for preprovisioned test identities, not an external OIDC certification.
func exerciseS4Browser(t *testing.T, ctx context.Context, owner *pgxpool.Pool, dsn string) {
	t.Helper()
	root, err := filepath.Abs("../../..")
	must(t, err)
	for _, app := range []string{"admin", "console"} {
		if _, err = os.Stat(filepath.Join(root, "frontend", "apps", app, "dist", "index.html")); err != nil {
			t.Fatal("S4 browser verification requires pnpm build first")
		}
	}
	browserDB, _ := openTemporaryRole(t, ctx, owner, dsn, "mender_s4_browser_", migrations.GrantBrowserSession)
	publisherDB, _ := openTemporaryRole(t, ctx, owner, dsn, "mender_s4_publisher_", migrations.GrantPublisherManager)
	reviewerDB, _ := openTemporaryRole(t, ctx, owner, dsn, "mender_s4_reviewer_", migrations.GrantGovernanceReviewer)
	releaseDB, _ := openTemporaryRole(t, ctx, owner, dsn, "mender_s4_release_", migrations.GrantReleaseManager)
	dangerousDB, _ := openTemporaryRole(t, ctx, owner, dsn, "mender_s4_dangerous_", migrations.GrantDangerousOperationManager)
	supportDB, _ := openTemporaryRole(t, ctx, owner, dsn, "mender_s4_support_", migrations.GrantSupportReader)
	platformDB, _ := openTemporaryRole(t, ctx, owner, dsn, "mender_s4_platform_", migrations.GrantPlatformAdminManager)
	billingDB, _ := openTemporaryRole(t, ctx, owner, dsn, "mender_s4_billing_", migrations.GrantBillingManager)
	paymentDB, _ := openTemporaryRole(t, ctx, owner, dsn, "mender_s4_payment_", migrations.GrantPaymentManager)
	clock := s4BrowserClock{}
	human, err := identityapp.NewHumanSessionService(identitypg.NewHumanSessions(browserDB), sessioncodec.Codec{}, clock, time.Hour)
	must(t, err)
	base := clock.Now().Truncate(time.Microsecond).Add(-time.Minute)
	credentials := map[string]map[string]string{}
	for _, person := range []struct{ name, role, workspace, staff string }{
		{"maker", "admin", "ws_s4_browser", "operator"}, {"checker", "admin", "ws_s4_browser", "reviewer"},
		{"support", "viewer", "ws_s4_support_home", "support"}, {"viewer", "viewer", "ws_s4_browser", ""},
	} {
		uid := "user_s4_" + person.name
		must(t, identitypg.New(owner).ProvisionHuman(ctx, identitypg.HumanProvision{UserID: uid, DisplayName: "S4 " + person.name, Issuer: "https://s4-issuer.example.test", Subject: uid, WorkspaceID: person.workspace, Role: identitydomain.MembershipRole(person.role), CreatedAt: base}))
		if person.staff != "" {
			_, err = owner.Exec(ctx, `INSERT INTO identity.platform_staff(user_id,role,created_at) VALUES($1,$2,$3)`, uid, person.staff, base)
			must(t, err)
		}
		issued, e := human.IssueVerified(ctx, identityapp.VerifiedOIDCIdentity{Issuer: "https://s4-issuer.example.test", Subject: uid})
		must(t, e)
		credentials[person.name] = map[string]string{"session": issued.SessionToken, "csrf": issued.CSRFToken, "user": uid}
	}
	_, err = owner.Exec(ctx, `INSERT INTO identity.workspaces(id,created_at) VALUES('ws_s4_freeze',$1)`, base)
	must(t, err)
	seedSettledBillingRun(t, ctx, owner, "ws_s4_browser", "run_s4_billing", "s4_browser", 100, 100, base)
	var chargeID string
	must(t, owner.QueryRow(ctx, `SELECT id FROM commerce.billing_journals WHERE workspace_id='ws_s4_browser' AND journal_kind='charge'`).Scan(&chargeID))
	// Reuse the already exercised reviewed Agent deployment; no real upstream is contacted.
	var exists int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM supply.deployments WHERE revision='deploy_s4a_agent'`).Scan(&exists))
	if exists != 1 {
		t.Fatal("S4 browser requires the reviewed publication fixture")
	}
	humanIdentity := identityfacade.NewHuman(human)
	pubAuth := supplyidentity.NewHuman(humanIdentity)
	govAuth := governanceidentity.New(humanIdentity)
	billAuth := commerceidentity.NewBilling(humanIdentity)
	router := gin.New()
	register := func(handler interface{ Register(*gin.Engine) }, e error) { must(t, e); handler.Register(router) }
	pubRepo := supplypg.NewPublicationRepository(publisherDB)
	pub, err := supplyapp.NewPublisherService(pubRepo, pubAuth, supplyhash.New())
	must(t, err)
	register(supplyhttp.NewPublication(pub, pubAuth))
	workflow, err := supplyapp.NewPublicationWorkflow(pubRepo, pubAuth, supplyrandom.PublicationIDs{}, clock)
	must(t, err)
	register(supplyhttp.NewPublicationWorkflow(workflow, pubAuth))
	review, err := governanceapp.NewPluginPublicationReview(governancepg.NewPublicationReview(reviewerDB), govAuth, clock)
	must(t, err)
	register(governancehttp.NewPluginPublicationReview(review, govAuth))
	release, err := supplyapp.NewReleaseGovernance(supplypg.NewReleaseGovernanceRepository(releaseDB), pubAuth, supplyrandom.ReleaseIDs{}, clock)
	must(t, err)
	register(supplyhttp.NewReleaseGovernance(release, pubAuth))
	dangerous, err := governanceapp.NewDangerousOperationService(governancepg.NewDangerousOperation(dangerousDB), govAuth, governancerandom.DangerousOperationIDs{}, clock)
	must(t, err)
	register(governancehttp.NewDangerousOperation(dangerous, govAuth))
	support, err := governanceapp.NewSupportAccessService(governancepg.NewDangerousOperation(dangerousDB), governancepg.NewSupportRead(supportDB), govAuth, governancerandom.DangerousOperationIDs{}, clock)
	must(t, err)
	register(governancehttp.NewSupportAccess(support, govAuth))
	platform, err := governanceapp.NewPlatformAdminService(governancepg.NewPlatformAdmin(platformDB), govAuth, governancerandom.DangerousOperationIDs{}, clock)
	must(t, err)
	register(governancehttp.NewPlatformAdmin(platform, govAuth))
	billing, err := commerceapp.NewBillingService(commercepg.NewBilling(billingDB), billAuth, clock)
	must(t, err)
	register(commercehttp.NewBilling(billing, billAuth))
	payment, err := commerceapp.NewPaymentAdminService(commercepg.NewPayments(paymentDB), billAuth, commercerandom.PaymentIDs{}, clock)
	must(t, err)
	register(commercehttp.NewPaymentAdmin(payment, billAuth))
	router.GET("/api/console/v1/session", func(c *gin.Context) {
		raw, e := c.Cookie("mender_session")
		if e != nil {
			c.Status(401)
			return
		}
		p, e := human.Authenticate(c.Request.Context(), raw)
		if e != nil {
			c.Status(401)
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"user_id": p.UserID}})
	})
	serve := func(app string) *httptest.Server {
		dir := filepath.Join(root, "frontend", "apps", app, "dist")
		files := http.FileServer(http.Dir(dir))
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if strings.HasPrefix(r.URL.Path, "/api/") {
				router.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/favicon.ico" {
				files.ServeHTTP(w, r)
				return
			}
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
		}))
	}
	adminServer, consoleServer := serve("admin"), serve("console")
	defer adminServer.Close()
	defer consoleServer.Close()
	encoded, err := json.Marshal(credentials)
	must(t, err)
	node, err := exec.LookPath("node")
	must(t, err)
	command := exec.CommandContext(ctx, node, "scripts/s4-browser-runner.mjs")
	command.Dir = root
	command.Env = append(os.Environ(), "MENDER_S4_TEST_SESSIONS="+string(encoded), "MENDER_S4_ADMIN_URL="+adminServer.URL, "MENDER_S4_CONSOLE_URL="+consoleServer.URL, "MENDER_S4_CHARGE_ID="+chargeID, "PLAYWRIGHT_BROWSERS_PATH="+filepath.Join(root, ".tmp", "playwright-browsers"))
	output, err := command.CombinedOutput()
	// Browser runner never echoes sessions; additionally redact before a failure log.
	safe := string(output)
	for _, credential := range credentials {
		safe = strings.ReplaceAll(safe, credential["session"], "[session-redacted]")
		safe = strings.ReplaceAll(safe, credential["csrf"], "[csrf-redacted]")
	}
	t.Log(safe)
	if err != nil {
		t.Fatal("S4 browser + actual HTTP + isolated PostgreSQL workflow failed", err)
	}
	var memberships int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM identity.workspace_memberships WHERE workspace_id='ws_s4_browser' AND user_id='user_s4_support'`).Scan(&memberships))
	if memberships != 0 {
		t.Fatal("JIT created forbidden target tenant membership")
	}
}
