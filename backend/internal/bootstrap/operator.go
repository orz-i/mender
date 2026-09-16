package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"github.com/orz-i/mender/backend/internal/platform/configenv"
	"io"
	"strings"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
	"github.com/orz-i/mender/backend/internal/contexts/identity/domain"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

// RunOperator is local, privileged administration, not a public registration endpoint.
// Only issue-key intentionally prints a generated secret, once, after durable insertion.
func RunOperator(ctx context.Context, args []string, getenv func(string) string, out, errOut io.Writer) error {
	resolved, resolveErr := configenv.Resolve(getenv)
	if resolveErr != nil {
		return resolveErr
	}
	getenv = resolved
	if len(args) == 0 {
		return errors.New("operator requires migrate, grant-runtime, grant-browser-session, grant-connection-manager, grant-oauth-refresher, grant-catalog-manager, grant-publisher-manager, grant-release-manager, grant-dangerous-operation-manager, grant-support-reader, grant-platform-admin-manager, grant-billing-manager, grant-payment-manager, grant-payment-callback-ingestor, grant-governance-reviewer, grant-governance-policy-manager, grant-governance-execution-confirmer, grant-commerce-observer, grant-admission, grant-cancellation, grant-worker, grant-executor, grant-mcp-connector, grant-reconciler, grant-callback-ingestor, grant-callback-observer, grant-settlement, grant-artifact-materializer, grant-artifact-object-reader, provision-human, provision-platform-staff, issue-key or revoke-key")
	}
	command := args[0]
	if command != "migrate" && command != "grant-runtime" && command != "grant-browser-session" && command != "grant-connection-manager" && command != "grant-oauth-refresher" && command != "grant-catalog-manager" && command != "grant-publisher-manager" && command != "grant-release-manager" && command != "grant-dangerous-operation-manager" && command != "grant-support-reader" && command != "grant-platform-admin-manager" && command != "grant-billing-manager" && command != "grant-payment-manager" && command != "grant-payment-callback-ingestor" && command != "grant-governance-reviewer" && command != "grant-governance-policy-manager" && command != "grant-governance-execution-confirmer" && command != "grant-commerce-observer" && command != "grant-admission" && command != "grant-cancellation" && command != "grant-worker" && command != "grant-executor" && command != "grant-mcp-connector" && command != "grant-reconciler" && command != "grant-callback-ingestor" && command != "grant-callback-observer" && command != "grant-settlement" && command != "grant-artifact-materializer" && command != "grant-artifact-object-reader" && command != "provision-human" && command != "provision-platform-staff" && command != "issue-key" && command != "revoke-key" {
		return errors.New("unknown operator command")
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(errOut)
	workspace := flags.String("workspace", "", "Workspace ID")
	subject := flags.String("subject", "", "service account ID")
	scopes := flags.String("scopes", "run:read", "comma-separated run:read,run:cancel,run:create")
	ttl := flags.Duration("ttl", 24*time.Hour, "credential lifetime (1 minute to 90 days)")
	role := flags.String("role", "", "existing restricted PostgreSQL login role")
	id := flags.String("id", "", "key ID to revoke (never the raw secret)")
	userID := flags.String("user", "", "human user ID")
	displayName := flags.String("display-name", "", "human display name")
	issuer := flags.String("issuer", "", "reviewed OIDC issuer URL")
	oidcSubject := flags.String("oidc-subject", "", "OIDC subject")
	membershipRole := flags.String("membership-role", "viewer", "owner, admin, developer or viewer")
	platformRole := flags.String("platform-role", "support", "support, reviewer, operator or auditor")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	var record domain.Credential
	var raw string
	if command == "issue-key" {
		if *ttl < time.Minute || *ttl > 90*24*time.Hour {
			return errors.New("invalid credential lifetime")
		}
		token, keyID, digest, err := (keycodec.Codec{}).Generate()
		if err != nil {
			return errors.New("credential randomness unavailable")
		}
		raw = token
		at := time.Now().UTC().Truncate(time.Microsecond)
		record = domain.Credential{ID: keyID, WorkspaceID: *workspace, SubjectID: *subject, Digest: digest, Scopes: strings.Split(*scopes, ","), CreatedAt: at, ExpiresAt: at.Add(*ttl)}
		if err = record.Validate(); err != nil {
			return err
		}
	}
	dsn := getenv("MENDER_ADMIN_DATABASE_URL")
	if dsn == "" {
		return errors.New("MENDER_ADMIN_DATABASE_URL is required; API runtime credentials are not used for administration")
	}
	pool, err := database.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	if command == "migrate" {
		if err = migrations.Apply(ctx, pool); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Mender schema migrations applied.\n")
		return err
	}
	if err = migrations.Verify(ctx, pool); err != nil {
		return err
	}
	switch command {
	case "grant-commerce-observer":
		if err = migrations.GrantCommerceObserver(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted commerce-observer grants applied.\n")
		return err
	case "grant-connection-manager":
		if err = migrations.GrantConnectionManager(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted connection-manager grants applied.\n")
		return err
	case "grant-oauth-refresher":
		if err = migrations.GrantOAuthRefresher(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted oauth-refresher grants applied.\n")
		return err
	case "grant-catalog-manager":
		if err = migrations.GrantCatalogManager(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted catalog-manager grants applied.\n")
		return err
	case "grant-publisher-manager":
		if err = migrations.GrantPublisherManager(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted publisher-manager grants applied.\n")
		return err
	case "grant-release-manager":
		if err = migrations.GrantReleaseManager(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted release-manager grants applied.\n")
		return err
	case "grant-dangerous-operation-manager":
		if err = migrations.GrantDangerousOperationManager(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted dangerous-operation-manager grants applied.\n")
		return err
	case "grant-support-reader":
		if err = migrations.GrantSupportReader(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted support-reader grants applied.\n")
		return err
	case "grant-platform-admin-manager":
		if err = migrations.GrantPlatformAdminManager(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted platform-admin-manager grants applied.\n")
		return err
	case "grant-billing-manager":
		if err = migrations.GrantBillingManager(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted billing-manager grants applied.\n")
		return err
	case "grant-payment-manager":
		if err = migrations.GrantPaymentManager(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted payment-manager grants applied.\n")
		return err
	case "grant-payment-callback-ingestor":
		if err = migrations.GrantPaymentCallbackIngestor(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted payment-callback-ingestor grants applied.\n")
		return err
	case "grant-governance-reviewer":
		if err = migrations.GrantGovernanceReviewer(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted governance-reviewer grants applied.\n")
		return err
	case "grant-governance-policy-manager":
		if err = migrations.GrantGovernancePolicyManager(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted governance-policy-manager grants applied.\n")
		return err
	case "grant-governance-execution-confirmer":
		if err = migrations.GrantGovernanceExecutionConfirmer(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted governance-execution-confirmer grants applied.\n")
		return err
	case "provision-human":
		value := identitypg.HumanProvision{UserID: *userID, DisplayName: *displayName, Issuer: *issuer, Subject: *oidcSubject, WorkspaceID: *workspace, Role: domain.MembershipRole(*membershipRole), CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		if err = identitypg.New(pool).ProvisionHuman(ctx, value); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Human OIDC identity and Workspace membership provisioned.\n")
		return err
	case "provision-platform-staff":
		value := identitypg.PlatformStaffProvision{UserID: *userID, Role: domain.PlatformStaffRole(*platformRole), CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		if err = identitypg.New(pool).ProvisionPlatformStaff(ctx, value); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Platform Staff role provisioned without Workspace membership.\n")
		return err
	case "grant-browser-session":
		if err = migrations.GrantBrowserSession(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted browser-session grants applied.\n")
		return err
	case "grant-mcp-connector":
		if err = migrations.GrantMCPConnector(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted upstream-MCP connector grants applied.\n")
		return err
	case "grant-settlement":
		if err = migrations.GrantSettlement(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted usage-settlement grants applied.\n")
		return err
	case "grant-artifact-materializer":
		if err = migrations.GrantArtifactMaterializer(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted Artifact object materializer grants applied.\n")
		return err
	case "grant-artifact-object-reader":
		if err = migrations.GrantArtifactObjectReader(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted Artifact object reader grants applied.\n")
		return err
	case "grant-reconciler":
		if err = migrations.GrantReconciler(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted provider-result reconciler grants applied.\n")
		return err
	case "grant-callback-ingestor":
		if err = migrations.GrantCallbackIngestor(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted provider-callback ingestor grants applied.\n")
		return err
	case "grant-callback-observer":
		if err = migrations.GrantCallbackObserver(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted provider-callback observer grants applied.\n")
		return err
	case "grant-executor":
		if err = migrations.GrantExecutor(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted executor-runtime grants applied; runtime startup must verify the target role before resolving supplier input.\n")
		return err
	case "grant-worker":
		if err = migrations.GrantWorker(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted worker-control grants applied; worker startup verifies the target role before enabling control.\n")
		return err
	case "grant-admission":
		if err = migrations.GrantAdmission(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted admission grants applied; StartRun startup verifies the target role before enabling the feature.\n")
		return err
	case "grant-cancellation":
		if err = migrations.GrantCancellation(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted cancellation grants applied; API startup verifies the target role before enabling the feature.\n")
		return err
	case "grant-runtime":
		if err = migrations.GrantRuntime(ctx, pool, *role); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Restricted runtime grants applied.\n")
		return err
	case "revoke-key":
		if err = identitypg.New(pool).Revoke(ctx, *id); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Credential revoked.\n")
		return err
	case "issue-key":
		if err = identitypg.New(pool).Provision(ctx, record); err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(struct {
			KeyID     string    `json:"key_id"`
			APIKey    string    `json:"api_key"`
			ExpiresAt time.Time `json:"expires_at"`
		}{record.ID, raw, record.ExpiresAt})
	}
	return errors.New("operator command not implemented")
}
