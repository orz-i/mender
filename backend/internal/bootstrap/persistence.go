package bootstrap

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	commercehttp "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/inbound/httpapi"
	commerceidentityaccess "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/outbound/identityaccess"
	commercepg "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/outbound/postgres"
	commerceapp "github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	connectionhttp "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/inbound/httpapi"
	connectionidentityaccess "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/identityaccess"
	connectionoauth "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/oauth"
	connectionpg "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/postgres"
	connectionrandom "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/random"
	connectionsupplycredentials "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/supplycredentials"
	connectionapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
	runfacade "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/facade"
	runhttp "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/httpapi"
	runartifactcap "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/artifactcap"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/coordinatedcancel"
	runcursor "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/cursor"
	runidentityaccess "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/identityaccess"
	runobjectfs "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/objectfs"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	governancehttp "github.com/orz-i/mender/backend/internal/contexts/governance/adapters/inbound/httpapi"
	governanceidentity "github.com/orz-i/mender/backend/internal/contexts/governance/adapters/outbound/identityaccess"
	governancepg "github.com/orz-i/mender/backend/internal/contexts/governance/adapters/outbound/postgres"
	governancerandom "github.com/orz-i/mender/backend/internal/contexts/governance/adapters/outbound/random"
	governanceapp "github.com/orz-i/mender/backend/internal/contexts/governance/application"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/inbound/facade"
	identityhttp "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/inbound/httpapi"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	identityoidc "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/oidc"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/sessioncodec"
	identityapp "github.com/orz-i/mender/backend/internal/contexts/identity/application"
	supplyhttp "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/inbound/httpapi"
	"github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/filesecret"
	supplyidentity "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/identityaccess"
	supplymanifesthash "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/manifesthash"
	supplypg "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/postgres"
	supplyrandom "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/random"
	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
	"github.com/orz-i/mender/backend/internal/platform/httpserver"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	cancelfacade "github.com/orz-i/mender/backend/internal/processes/admission/adapters/inbound/cancellation"
	admissionfacade "github.com/orz-i/mender/backend/internal/processes/admission/adapters/inbound/facade"
	admissionhttp "github.com/orz-i/mender/backend/internal/processes/admission/adapters/inbound/httpapi"
	admissionidentityaccess "github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/identityaccess"
	"github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/identitycancel"
	admissionidentitystart "github.com/orz-i/mender/backend/internal/processes/admission/adapters/outbound/identitystart"
	admissionapp "github.com/orz-i/mender/backend/internal/processes/admission/application"
	catalogmanagementhttp "github.com/orz-i/mender/backend/internal/processes/catalogmanagement/adapters/inbound/httpapi"
	catalogmanagementidentity "github.com/orz-i/mender/backend/internal/processes/catalogmanagement/adapters/outbound/identityaccess"
	catalogmanagementopenapi "github.com/orz-i/mender/backend/internal/processes/catalogmanagement/adapters/outbound/openapiaccess"
	catalogmanagementpg "github.com/orz-i/mender/backend/internal/processes/catalogmanagement/adapters/outbound/postgres"
	catalogmanagementrandom "github.com/orz-i/mender/backend/internal/processes/catalogmanagement/adapters/outbound/random"
	catalogmanagementapp "github.com/orz-i/mender/backend/internal/processes/catalogmanagement/application"
	consolelaunchhttp "github.com/orz-i/mender/backend/internal/processes/consolelaunch/adapters/inbound/httpapi"
	consolelaunchidentity "github.com/orz-i/mender/backend/internal/processes/consolelaunch/adapters/outbound/identityaccess"
	consolelaunchpg "github.com/orz-i/mender/backend/internal/processes/consolelaunch/adapters/outbound/postgres"
	consolelaunchapp "github.com/orz-i/mender/backend/internal/processes/consolelaunch/application"
	mcphttp "github.com/orz-i/mender/backend/internal/processes/mcpbridge/adapters/inbound/httpapi"
	mcpadmission "github.com/orz-i/mender/backend/internal/processes/mcpbridge/adapters/outbound/admissionaccess"
	mcpexecution "github.com/orz-i/mender/backend/internal/processes/mcpbridge/adapters/outbound/executionaccess"
	mcpidentity "github.com/orz-i/mender/backend/internal/processes/mcpbridge/adapters/outbound/identityaccess"
	mcpapp "github.com/orz-i/mender/backend/internal/processes/mcpbridge/application"
	openapifacade "github.com/orz-i/mender/backend/internal/processes/openapiimport/adapters/inbound/facade"
	callbackhttp "github.com/orz-i/mender/backend/internal/processes/providercallback/adapters/inbound/httpapi"
	callbackexecution "github.com/orz-i/mender/backend/internal/processes/providercallback/adapters/outbound/executionaccess"
	callbackfilesecret "github.com/orz-i/mender/backend/internal/processes/providercallback/adapters/outbound/filesecret"
	callbackverify "github.com/orz-i/mender/backend/internal/processes/providercallback/adapters/outbound/hmacverify"
	callbackapp "github.com/orz-i/mender/backend/internal/processes/providercallback/application"
	"github.com/orz-i/mender/backend/migrations"
)

type APIConfig struct {
	RunAPIEnabled                           bool
	RunReadAPIEnabled                       bool
	AgentInputAPIEnabled                    bool
	AgentInputDatabaseURL                   string
	AgentInputExecutorDatabaseURL           string
	AgentInputSecretRoot                    string
	AgentInputProviderIDs                   []string
	AgentInputAllowedHosts                  []string
	AgentInputAllowHTTP                     bool
	AgentInputAllowLoopback                 bool
	DatabaseURL                             string
	CursorSigningKey                        []byte
	ArtifactObjectReadEnabled               bool
	ArtifactObjectDatabaseURL               string
	ArtifactObjectRoot                      string
	ArtifactObjectSigningKeyFile            string
	ArtifactObjectCapabilityTTL             time.Duration
	CoordinatedCancelEnabled                bool
	ProviderCancelEnabled                   bool
	CancellationDatabaseURL                 string
	ProviderCallbackEnabled                 bool
	ProviderCallbackDatabaseURL             string
	ProviderCallbackSecretRoot              string
	ProviderCallbackReviewedKeys            map[string][]string
	StartRunAPIEnabled                      bool
	AdmissionDatabaseURL                    string
	MCPGatewayEnabled                       bool
	MCPFixedToolsetEnabled                  bool
	ConsoleOIDCEnabled                      bool
	BrowserSessionDatabaseURL               string
	OIDCIssuer                              string
	OIDCClientID                            string
	OIDCClientSecretFile                    string
	OIDCRedirectURL                         string
	FlowSigningKeyFile                      string
	ConsoleCookieSecure                     bool
	ConsoleSessionTTL                       time.Duration
	ConsoleRunDelegationEnabled             bool
	ConsoleRunDelegationTTL                 time.Duration
	ConsoleHumanStartEnabled                bool
	ConsoleHumanStartDelegationTTL          time.Duration
	ConsoleLaunchDiscoveryEnabled           bool
	ConsoleUsageEnabled                     bool
	CommerceObserverDatabaseURL             string
	ConsoleConnectionsEnabled               bool
	ConnectionManagerDatabaseURL            string
	ConsoleCatalogEnabled                   bool
	CatalogManagerDatabaseURL               string
	ConsolePublisherEnabled                 bool
	PublisherManagerDatabaseURL             string
	AdminReleaseGovernanceEnabled           bool
	ReleaseManagerDatabaseURL               string
	AdminDangerousOperationEnabled          bool
	DangerousOperationManagerDatabaseURL    string
	AdminSupportAccessEnabled               bool
	SupportReaderDatabaseURL                string
	AdminCatalogReviewEnabled               bool
	AdminPluginReviewEnabled                bool
	GovernanceReviewerDatabaseURL           string
	AdminCatalogPolicyEnabled               bool
	GovernancePolicyManagerDatabaseURL      string
	AdminProviderCallbacksEnabled           bool
	CallbackObserverDatabaseURL             string
	ConsoleExecutionRiskEnabled             bool
	GovernanceExecutionConfirmerDatabaseURL string
	ConsoleConnectionOAuthEnabled           bool
	ConnectionOAuthRefreshEnabled           bool
	ConnectionOAuthProviderID               string
	ConnectionOAuthAuthorizationURL         string
	ConnectionOAuthTokenURL                 string
	ConnectionOAuthClientID                 string
	ConnectionOAuthClientSecretFile         string
	ConnectionOAuthRedirectURL              string
	ConnectionOAuthScopes                   []string
	ConnectionOAuthSecretRoot               string
	ConnectionOAuthFlowTTL                  time.Duration
}

func loadMountedTextSecret(path, label string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New(label + " unavailable")
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || len(value) > 4096 {
		return "", errors.New(label + " unavailable")
	}
	return value, nil
}

func loadConsoleSecrets(clientSecretFile, signingKeyFile string) (string, []byte, error) {
	clientSecretRaw, err := os.ReadFile(clientSecretFile)
	if err != nil {
		return "", nil, errors.New("OIDC client secret unavailable")
	}
	clientSecret := strings.TrimSpace(string(clientSecretRaw))
	if clientSecret == "" || len(clientSecret) > 4096 {
		return "", nil, errors.New("OIDC client secret unavailable")
	}
	keyRaw, err := os.ReadFile(signingKeyFile)
	if err != nil {
		return "", nil, errors.New("OIDC flow signing key unavailable")
	}
	encoded := strings.TrimSpace(string(keyRaw))
	key, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(encoded) != 43 || len(key) != 32 || base64.RawURLEncoding.EncodeToString(key) != encoded {
		return "", nil, errors.New("OIDC flow signing key must be 32-byte unpadded base64url")
	}
	return clientSecret, key, nil
}

func parseReviewedCallbackKeys(raw string) (map[string][]string, error) {
	parts := strings.Split(raw, ",")
	if len(parts) < 1 || len(parts) > 256 {
		return nil, errors.New("provider callback keys require 1-256 reviewed provider entries")
	}
	result := make(map[string][]string, len(parts))
	validID := func(value string) bool {
		if len(value) < 1 || len(value) > 128 {
			return false
		}
		for _, ch := range value {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
				return false
			}
		}
		return true
	}
	for _, part := range parts {
		pair := strings.Split(strings.TrimSpace(part), "=")
		if len(pair) != 2 || !validID(pair[0]) {
			return nil, errors.New("provider callback keys must use provider=key_id[|key_id] entries")
		}
		if _, duplicate := result[pair[0]]; duplicate {
			return nil, errors.New("provider callback keys contain duplicate provider")
		}
		keys := strings.Split(pair[1], "|")
		if len(keys) < 1 || len(keys) > 2 {
			return nil, errors.New("provider callback keys allow one or two reviewed key IDs per provider")
		}
		seen := map[string]bool{}
		for i, key := range keys {
			key = strings.TrimSpace(key)
			if !validID(key) || seen[key] {
				return nil, errors.New("provider callback keys contain invalid or duplicate key ID")
			}
			seen[key] = true
			keys[i] = key
		}
		result[pair[0]] = keys
	}
	return result, nil
}

func LoadAPIConfig(getenv func(string) string) (APIConfig, error) {
	c := APIConfig{ConsoleCookieSecure: true, ConsoleSessionTTL: 8 * time.Hour, ConsoleRunDelegationTTL: 10 * time.Minute, ConsoleHumanStartDelegationTTL: 5 * time.Minute, ConnectionOAuthFlowTTL: 10 * time.Minute}
	switch getenv("MENDER_CONSOLE_OIDC_ENABLED") {
	case "", "false":
	case "true":
		c.ConsoleOIDCEnabled = true
		c.BrowserSessionDatabaseURL = getenv("MENDER_BROWSER_SESSION_DATABASE_URL")
		c.OIDCIssuer = strings.TrimSpace(getenv("MENDER_CONSOLE_OIDC_ISSUER"))
		c.OIDCClientID = strings.TrimSpace(getenv("MENDER_CONSOLE_OIDC_CLIENT_ID"))
		c.OIDCClientSecretFile = strings.TrimSpace(getenv("MENDER_CONSOLE_OIDC_CLIENT_SECRET_FILE"))
		c.OIDCRedirectURL = strings.TrimSpace(getenv("MENDER_CONSOLE_OIDC_REDIRECT_URL"))
		c.FlowSigningKeyFile = strings.TrimSpace(getenv("MENDER_CONSOLE_FLOW_SIGNING_KEY_FILE"))
		if c.BrowserSessionDatabaseURL == "" || c.OIDCIssuer == "" || c.OIDCClientID == "" || c.OIDCClientSecretFile == "" || c.OIDCRedirectURL == "" || c.FlowSigningKeyFile == "" {
			return APIConfig{}, errors.New("Console OIDC requires session database, issuer, client, redirect and mounted secret files")
		}
		switch getenv("MENDER_CONSOLE_COOKIE_SECURE") {
		case "", "true":
		case "false":
			c.ConsoleCookieSecure = false
			if !(strings.HasPrefix(c.OIDCRedirectURL, "http://127.0.0.1:") || strings.HasPrefix(c.OIDCRedirectURL, "http://localhost:")) {
				return APIConfig{}, errors.New("insecure Console cookie is only allowed for loopback OIDC redirect")
			}
		default:
			return APIConfig{}, errors.New("MENDER_CONSOLE_COOKIE_SECURE must be true or false")
		}
		if raw := getenv("MENDER_CONSOLE_SESSION_TTL"); raw != "" {
			ttl, err := time.ParseDuration(raw)
			if err != nil || ttl < 5*time.Minute || ttl > 24*time.Hour {
				return APIConfig{}, errors.New("Console session TTL must be between 5m and 24h")
			}
			c.ConsoleSessionTTL = ttl
		}
	default:
		return c, errors.New("MENDER_CONSOLE_OIDC_ENABLED must be true or false")
	}
	switch getenv("MENDER_CONSOLE_CONNECTIONS_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleOIDCEnabled {
			return APIConfig{}, errors.New("Console Connections require Console OIDC")
		}
		c.ConsoleConnectionsEnabled = true
		c.ConnectionManagerDatabaseURL = getenv("MENDER_CONNECTION_MANAGER_DATABASE_URL")
		if c.ConnectionManagerDatabaseURL == "" {
			return APIConfig{}, errors.New("Console Connections require a separate connection-manager database role")
		}
	default:
		return c, errors.New("MENDER_CONSOLE_CONNECTIONS_ENABLED must be true or false")
	}
	switch getenv("MENDER_CONSOLE_CATALOG_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleOIDCEnabled {
			return APIConfig{}, errors.New("Console Catalog requires Console OIDC")
		}
		c.ConsoleCatalogEnabled = true
		c.CatalogManagerDatabaseURL = getenv("MENDER_CATALOG_MANAGER_DATABASE_URL")
		if c.CatalogManagerDatabaseURL == "" {
			return APIConfig{}, errors.New("Console Catalog requires a separate catalog-manager database role")
		}
	default:
		return c, errors.New("MENDER_CONSOLE_CATALOG_ENABLED must be true or false")
	}
	switch getenv("MENDER_CONSOLE_PUBLISHER_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleOIDCEnabled {
			return APIConfig{}, errors.New("Console Publisher requires Console OIDC")
		}
		c.ConsolePublisherEnabled = true
		c.PublisherManagerDatabaseURL = strings.TrimSpace(getenv("MENDER_PUBLISHER_MANAGER_DATABASE_URL"))
		if c.PublisherManagerDatabaseURL == "" {
			return APIConfig{}, errors.New("Console Publisher requires a separate publisher-manager database role")
		}
	default:
		return c, errors.New("MENDER_CONSOLE_PUBLISHER_ENABLED must be true or false")
	}
	switch getenv("MENDER_ADMIN_RELEASE_GOVERNANCE_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleOIDCEnabled {
			return APIConfig{}, errors.New("Admin Release Governance requires Console OIDC")
		}
		c.AdminReleaseGovernanceEnabled = true
		c.ReleaseManagerDatabaseURL = strings.TrimSpace(getenv("MENDER_RELEASE_MANAGER_DATABASE_URL"))
		if c.ReleaseManagerDatabaseURL == "" {
			return APIConfig{}, errors.New("Admin Release Governance requires a separate release-manager database role")
		}
	default:
		return c, errors.New("MENDER_ADMIN_RELEASE_GOVERNANCE_ENABLED must be true or false")
	}
	switch getenv("MENDER_ADMIN_DANGEROUS_OPERATION_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleOIDCEnabled {
			return APIConfig{}, errors.New("Admin Dangerous Operation requires Console OIDC")
		}
		c.AdminDangerousOperationEnabled = true
		c.DangerousOperationManagerDatabaseURL = strings.TrimSpace(getenv("MENDER_DANGEROUS_OPERATION_MANAGER_DATABASE_URL"))
		if c.DangerousOperationManagerDatabaseURL == "" {
			return APIConfig{}, errors.New("Admin Dangerous Operation requires a separate dangerous-operation-manager database role")
		}
	default:
		return c, errors.New("MENDER_ADMIN_DANGEROUS_OPERATION_ENABLED must be true or false")
	}
	switch getenv("MENDER_ADMIN_SUPPORT_ACCESS_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleOIDCEnabled || !c.AdminDangerousOperationEnabled {
			return APIConfig{}, errors.New("Admin Support Access requires Console OIDC and Admin Dangerous Operation")
		}
		c.AdminSupportAccessEnabled = true
		c.SupportReaderDatabaseURL = strings.TrimSpace(getenv("MENDER_SUPPORT_READER_DATABASE_URL"))
		if c.SupportReaderDatabaseURL == "" {
			return APIConfig{}, errors.New("Admin Support Access requires a separate support-reader database role")
		}
	default:
		return c, errors.New("MENDER_ADMIN_SUPPORT_ACCESS_ENABLED must be true or false")
	}
	switch getenv("MENDER_ADMIN_CATALOG_REVIEW_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleOIDCEnabled || !c.ConsoleCatalogEnabled {
			return APIConfig{}, errors.New("Admin Catalog review requires Console OIDC and Console Catalog")
		}
		c.AdminCatalogReviewEnabled = true
		c.GovernanceReviewerDatabaseURL = getenv("MENDER_GOVERNANCE_REVIEWER_DATABASE_URL")
		if c.GovernanceReviewerDatabaseURL == "" {
			return APIConfig{}, errors.New("Admin Catalog review requires a separate governance-reviewer database role")
		}
	default:
		return c, errors.New("MENDER_ADMIN_CATALOG_REVIEW_ENABLED must be true or false")
	}
	switch getenv("MENDER_ADMIN_PLUGIN_REVIEW_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleOIDCEnabled || !c.ConsolePublisherEnabled {
			return APIConfig{}, errors.New("Admin Plugin review requires Console OIDC and Console Publisher")
		}
		c.AdminPluginReviewEnabled = true
		reviewerURL := strings.TrimSpace(getenv("MENDER_GOVERNANCE_REVIEWER_DATABASE_URL"))
		if reviewerURL == "" {
			return APIConfig{}, errors.New("Admin Plugin review requires a separate governance-reviewer database role")
		}
		if c.GovernanceReviewerDatabaseURL != "" && c.GovernanceReviewerDatabaseURL != reviewerURL {
			return APIConfig{}, errors.New("publication review features must share the configured governance-reviewer role")
		}
		c.GovernanceReviewerDatabaseURL = reviewerURL
	default:
		return c, errors.New("MENDER_ADMIN_PLUGIN_REVIEW_ENABLED must be true or false")
	}
	switch getenv("MENDER_ADMIN_CATALOG_POLICY_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleOIDCEnabled || !c.ConsoleCatalogEnabled {
			return APIConfig{}, errors.New("Admin Catalog policy requires Console OIDC and Console Catalog")
		}
		c.AdminCatalogPolicyEnabled = true
		c.GovernancePolicyManagerDatabaseURL = getenv("MENDER_GOVERNANCE_POLICY_MANAGER_DATABASE_URL")
		if c.GovernancePolicyManagerDatabaseURL == "" {
			return APIConfig{}, errors.New("Admin Catalog policy requires a separate governance-policy-manager database role")
		}
	default:
		return c, errors.New("MENDER_ADMIN_CATALOG_POLICY_ENABLED must be true or false")
	}
	switch getenv("MENDER_ADMIN_PROVIDER_CALLBACKS_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleOIDCEnabled {
			return APIConfig{}, errors.New("Admin Provider callbacks require Console OIDC")
		}
		c.AdminProviderCallbacksEnabled = true
		c.CallbackObserverDatabaseURL = strings.TrimSpace(getenv("MENDER_CALLBACK_OBSERVER_DATABASE_URL"))
		if c.CallbackObserverDatabaseURL == "" {
			return APIConfig{}, errors.New("Admin Provider callbacks require a separate callback-observer database role")
		}
	default:
		return c, errors.New("MENDER_ADMIN_PROVIDER_CALLBACKS_ENABLED must be true or false")
	}
	switch getenv("MENDER_CONSOLE_LAUNCH_DISCOVERY_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleOIDCEnabled {
			return APIConfig{}, errors.New("Console launch discovery requires Console OIDC")
		}
		c.ConsoleLaunchDiscoveryEnabled = true
	default:
		return c, errors.New("MENDER_CONSOLE_LAUNCH_DISCOVERY_ENABLED must be true or false")
	}
	switch getenv("MENDER_CONSOLE_USAGE_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleOIDCEnabled {
			return APIConfig{}, errors.New("Console Usage requires Console OIDC")
		}
		c.ConsoleUsageEnabled = true
		c.CommerceObserverDatabaseURL = getenv("MENDER_COMMERCE_OBSERVER_DATABASE_URL")
		if c.CommerceObserverDatabaseURL == "" {
			return APIConfig{}, errors.New("Console Usage requires a separate commerce-observer database role")
		}
	default:
		return c, errors.New("MENDER_CONSOLE_USAGE_ENABLED must be true or false")
	}
	switch getenv("MENDER_CONSOLE_CONNECTION_OAUTH_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleConnectionsEnabled || !c.ConsoleOIDCEnabled {
			return APIConfig{}, errors.New("Connection OAuth requires Console Connections and Console OIDC")
		}
		c.ConsoleConnectionOAuthEnabled = true
		c.ConnectionOAuthProviderID = strings.TrimSpace(getenv("MENDER_CONNECTION_OAUTH_PROVIDER_ID"))
		c.ConnectionOAuthAuthorizationURL = strings.TrimSpace(getenv("MENDER_CONNECTION_OAUTH_AUTHORIZATION_URL"))
		c.ConnectionOAuthTokenURL = strings.TrimSpace(getenv("MENDER_CONNECTION_OAUTH_TOKEN_URL"))
		c.ConnectionOAuthClientID = strings.TrimSpace(getenv("MENDER_CONNECTION_OAUTH_CLIENT_ID"))
		c.ConnectionOAuthClientSecretFile = strings.TrimSpace(getenv("MENDER_CONNECTION_OAUTH_CLIENT_SECRET_FILE"))
		c.ConnectionOAuthRedirectURL = strings.TrimSpace(getenv("MENDER_CONNECTION_OAUTH_REDIRECT_URL"))
		c.ConnectionOAuthSecretRoot = strings.TrimSpace(getenv("MENDER_CONNECTION_OAUTH_SECRET_ROOT"))
		for _, raw := range strings.Split(getenv("MENDER_CONNECTION_OAUTH_SCOPES"), ",") {
			scope := strings.TrimSpace(raw)
			if scope != "" {
				c.ConnectionOAuthScopes = append(c.ConnectionOAuthScopes, scope)
			}
		}
		if c.ConnectionOAuthProviderID == "" || c.ConnectionOAuthAuthorizationURL == "" || c.ConnectionOAuthTokenURL == "" || c.ConnectionOAuthClientID == "" || c.ConnectionOAuthClientSecretFile == "" || c.ConnectionOAuthRedirectURL == "" || c.ConnectionOAuthSecretRoot == "" || len(c.ConnectionOAuthScopes) == 0 {
			return APIConfig{}, errors.New("Connection OAuth requires one reviewed provider, endpoints, client secret file, redirect, scopes and secret root")
		}
		if raw := getenv("MENDER_CONNECTION_OAUTH_FLOW_TTL"); raw != "" {
			ttl, err := time.ParseDuration(raw)
			if err != nil || ttl < time.Minute || ttl > 15*time.Minute {
				return APIConfig{}, errors.New("Connection OAuth flow TTL must be between 1m and 15m")
			}
			c.ConnectionOAuthFlowTTL = ttl
		}
	default:
		return c, errors.New("MENDER_CONSOLE_CONNECTION_OAUTH_ENABLED must be true or false")
	}
	switch getenv("MENDER_CONNECTION_OAUTH_REFRESH_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleConnectionOAuthEnabled {
			return APIConfig{}, errors.New("Connection OAuth refresh requires Connection OAuth")
		}
		c.ConnectionOAuthRefreshEnabled = true
	default:
		return c, errors.New("MENDER_CONNECTION_OAUTH_REFRESH_ENABLED must be true or false")
	}
	switch getenv("MENDER_RUN_START_API_ENABLED") {
	case "", "false":
	case "true":
		c.StartRunAPIEnabled = true
		c.AdmissionDatabaseURL = getenv("MENDER_ADMISSION_DATABASE_URL")
		if c.AdmissionDatabaseURL == "" {
			return APIConfig{}, errors.New("StartRun API requires a separate admission database role")
		}
	default:
		return c, errors.New("MENDER_RUN_START_API_ENABLED must be true or false")
	}
	switch getenv("MENDER_CONSOLE_HUMAN_START_ENABLED") {
	case "", "false":
	case "true":
		c.ConsoleHumanStartEnabled = true
		if !c.ConsoleOIDCEnabled || !c.ConsoleLaunchDiscoveryEnabled || !c.StartRunAPIEnabled {
			return APIConfig{}, errors.New("Console Human StartRun requires Console OIDC, launch discovery and StartRun API")
		}
		if raw := getenv("MENDER_CONSOLE_HUMAN_START_DELEGATION_TTL"); raw != "" {
			ttl, err := time.ParseDuration(raw)
			if err != nil || ttl < time.Minute || ttl > 10*time.Minute {
				return APIConfig{}, errors.New("Console Human StartRun delegation TTL must be between 1m and 10m")
			}
			c.ConsoleHumanStartDelegationTTL = ttl
		}
	default:
		return c, errors.New("MENDER_CONSOLE_HUMAN_START_ENABLED must be true or false")
	}
	switch getenv("MENDER_CONSOLE_EXECUTION_RISK_ENABLED") {
	case "", "false":
	case "true":
		if !c.ConsoleOIDCEnabled || !c.ConsoleHumanStartEnabled || !c.ConsoleLaunchDiscoveryEnabled || !c.StartRunAPIEnabled {
			return APIConfig{}, errors.New("Console execution risk requires Console OIDC, launch discovery and Human StartRun")
		}
		c.ConsoleExecutionRiskEnabled = true
		c.GovernanceExecutionConfirmerDatabaseURL = getenv("MENDER_GOVERNANCE_EXECUTION_CONFIRMER_DATABASE_URL")
		if c.GovernanceExecutionConfirmerDatabaseURL == "" {
			return APIConfig{}, errors.New("Console execution risk requires a separate governance-execution-confirmer database role")
		}
	default:
		return c, errors.New("MENDER_CONSOLE_EXECUTION_RISK_ENABLED must be true or false")
	}
	switch getenv("MENDER_RUN_COORDINATED_CANCEL_ENABLED") {
	case "", "false":
	case "true":
		c.CoordinatedCancelEnabled = true
	default:
		return c, errors.New("MENDER_RUN_COORDINATED_CANCEL_ENABLED must be true or false")
	}
	switch getenv("MENDER_RUN_PROVIDER_CANCEL_ENABLED") {
	case "", "false":
	case "true":
		c.ProviderCancelEnabled = true
	default:
		return c, errors.New("MENDER_RUN_PROVIDER_CANCEL_ENABLED must be true or false")
	}
	if c.ProviderCancelEnabled && !c.CoordinatedCancelEnabled {
		return c, errors.New("provider cancellation requires coordinated cancellation")
	}
	if c.CoordinatedCancelEnabled {
		c.CancellationDatabaseURL = getenv("MENDER_CANCELLATION_DATABASE_URL")
		if c.CancellationDatabaseURL == "" {
			return APIConfig{}, errors.New("coordinated cancellation requires a separate cancellation database role")
		}
	}
	switch getenv("MENDER_PROVIDER_CALLBACK_ENABLED") {
	case "", "false":
	case "true":
		c.ProviderCallbackEnabled = true
		c.ProviderCallbackDatabaseURL = strings.TrimSpace(getenv("MENDER_CALLBACK_INGESTOR_DATABASE_URL"))
		c.ProviderCallbackSecretRoot = strings.TrimSpace(getenv("MENDER_PROVIDER_CALLBACK_SECRET_ROOT"))
		if c.ProviderCallbackDatabaseURL == "" || c.ProviderCallbackSecretRoot == "" {
			return APIConfig{}, errors.New("provider callbacks require callback-ingestor database URL and mounted secret root")
		}
		keys, err := parseReviewedCallbackKeys(strings.TrimSpace(getenv("MENDER_PROVIDER_CALLBACK_KEYS")))
		if err != nil {
			return APIConfig{}, err
		}
		c.ProviderCallbackReviewedKeys = keys
	default:
		return c, errors.New("MENDER_PROVIDER_CALLBACK_ENABLED must be true or false")
	}
	switch getenv("MENDER_RUN_READ_API_ENABLED") {
	case "", "false":
	case "true":
		c.RunReadAPIEnabled = true
	default:
		return c, errors.New("MENDER_RUN_READ_API_ENABLED must be true or false")
	}
	switch getenv("MENDER_AGENT_INPUT_API_ENABLED") {
	case "", "false":
	case "true":
		c.AgentInputAPIEnabled = true
		c.AgentInputDatabaseURL = strings.TrimSpace(getenv("MENDER_AGENT_INPUT_DATABASE_URL"))
		c.AgentInputExecutorDatabaseURL = strings.TrimSpace(getenv("MENDER_AGENT_INPUT_EXECUTOR_DATABASE_URL"))
		c.AgentInputSecretRoot = strings.TrimSpace(getenv("MENDER_AGENT_INPUT_SECRET_ROOT"))
		var err error
		c.AgentInputProviderIDs, err = reviewedList(getenv("MENDER_AGENT_INPUT_PROVIDER_IDS"), 256)
		if err != nil {
			return APIConfig{}, errors.New("Agent input API requires reviewed provider IDs")
		}
		c.AgentInputAllowedHosts, err = reviewedList(getenv("MENDER_AGENT_INPUT_ALLOWED_HOSTS"), 256)
		if err != nil {
			return APIConfig{}, errors.New("Agent input API requires an exact egress host allowlist")
		}
		c.AgentInputAllowHTTP, err = strictBool(getenv, "MENDER_AGENT_INPUT_ALLOW_HTTP")
		if err != nil {
			return APIConfig{}, err
		}
		c.AgentInputAllowLoopback, err = strictBool(getenv, "MENDER_AGENT_INPUT_ALLOW_LOOPBACK")
		if err != nil {
			return APIConfig{}, err
		}
		if !c.RunReadAPIEnabled || c.AgentInputDatabaseURL == "" || c.AgentInputExecutorDatabaseURL == "" || c.AgentInputSecretRoot == "" {
			return APIConfig{}, errors.New("Agent input API requires Run read API, input-sender/executor database roles and mounted secret root")
		}
	default:
		return c, errors.New("MENDER_AGENT_INPUT_API_ENABLED must be true or false")
	}
	switch getenv("MENDER_ARTIFACT_OBJECT_READ_ENABLED") {
	case "", "false":
	case "true":
		c.ArtifactObjectReadEnabled = true
		c.ArtifactObjectDatabaseURL = strings.TrimSpace(getenv("MENDER_ARTIFACT_OBJECT_READER_DATABASE_URL"))
		c.ArtifactObjectRoot = strings.TrimSpace(getenv("MENDER_ARTIFACT_OBJECT_ROOT"))
		c.ArtifactObjectSigningKeyFile = strings.TrimSpace(getenv("MENDER_ARTIFACT_OBJECT_SIGNING_KEY_FILE"))
		c.ArtifactObjectCapabilityTTL = 2 * time.Minute
		if raw := strings.TrimSpace(getenv("MENDER_ARTIFACT_OBJECT_CAPABILITY_TTL")); raw != "" {
			ttl, err := time.ParseDuration(raw)
			if err != nil || ttl < 30*time.Second || ttl > 5*time.Minute {
				return APIConfig{}, errors.New("Artifact object capability TTL must be between 30s and 5m")
			}
			c.ArtifactObjectCapabilityTTL = ttl
		}
		if !c.RunReadAPIEnabled || c.ArtifactObjectDatabaseURL == "" || c.ArtifactObjectRoot == "" || c.ArtifactObjectSigningKeyFile == "" {
			return APIConfig{}, errors.New("Artifact object reads require Run read API, reader database URL, object root and signing key file")
		}
		if _, err := runartifactcap.NewFromFile(c.ArtifactObjectSigningKeyFile); err != nil {
			return APIConfig{}, err
		}
		if _, err := runobjectfs.New(c.ArtifactObjectRoot); err != nil {
			return APIConfig{}, err
		}
	default:
		return c, errors.New("MENDER_ARTIFACT_OBJECT_READ_ENABLED must be true or false")
	}
	switch getenv("MENDER_CONSOLE_RUN_DELEGATION_ENABLED") {
	case "", "false":
	case "true":
		c.ConsoleRunDelegationEnabled = true
		if raw := getenv("MENDER_CONSOLE_RUN_DELEGATION_TTL"); raw != "" {
			ttl, err := time.ParseDuration(raw)
			if err != nil || ttl < time.Minute || ttl > 30*time.Minute {
				return APIConfig{}, errors.New("Console Run delegation TTL must be between 1m and 30m")
			}
			c.ConsoleRunDelegationTTL = ttl
		}
	default:
		return c, errors.New("MENDER_CONSOLE_RUN_DELEGATION_ENABLED must be true or false")
	}
	if c.ConsoleRunDelegationEnabled && (!c.ConsoleOIDCEnabled || !c.RunReadAPIEnabled) {
		return APIConfig{}, errors.New("Console Run delegation requires Console OIDC and Run read API")
	}
	switch getenv("MENDER_MCP_GATEWAY_ENABLED") {
	case "", "false":
	case "true":
		c.MCPGatewayEnabled = true
	default:
		return c, errors.New("MENDER_MCP_GATEWAY_ENABLED must be true or false")
	}
	if c.MCPGatewayEnabled && (!c.RunReadAPIEnabled || !c.StartRunAPIEnabled || !c.CoordinatedCancelEnabled) {
		return c, errors.New("MCP gateway requires Run read, StartRun and coordinated cancellation capabilities")
	}
	switch getenv("MENDER_MCP_FIXED_TOOLSET_ENABLED") {
	case "", "false":
	case "true":
		c.MCPFixedToolsetEnabled = true
	default:
		return c, errors.New("MENDER_MCP_FIXED_TOOLSET_ENABLED must be true or false")
	}
	if c.MCPFixedToolsetEnabled && !c.MCPGatewayEnabled {
		return c, errors.New("Fixed Toolset MCP requires the authenticated MCP gateway")
	}
	switch getenv("MENDER_RUN_API_ENABLED") {
	case "", "false":
		if c.RunReadAPIEnabled || c.AgentInputAPIEnabled || c.ArtifactObjectReadEnabled || c.CoordinatedCancelEnabled || c.ProviderCancelEnabled || c.ProviderCallbackEnabled || c.StartRunAPIEnabled || c.MCPGatewayEnabled || c.MCPFixedToolsetEnabled || c.ConsoleOIDCEnabled || c.ConsoleRunDelegationEnabled || c.ConsoleHumanStartEnabled || c.ConsoleExecutionRiskEnabled || c.ConsoleLaunchDiscoveryEnabled || c.ConsoleUsageEnabled || c.ConsoleConnectionsEnabled || c.ConsoleCatalogEnabled || c.AdminCatalogReviewEnabled || c.AdminCatalogPolicyEnabled || c.AdminProviderCallbacksEnabled || c.ConsoleConnectionOAuthEnabled {
			return APIConfig{}, errors.New("Run capabilities require the authenticated Run API")
		}
		return c, nil
	case "true":
		c.RunAPIEnabled = true
	default:
		return c, errors.New("MENDER_RUN_API_ENABLED must be true or false")
	}
	c.DatabaseURL = getenv("MENDER_DATABASE_URL")
	if c.DatabaseURL == "" {
		return APIConfig{}, errors.New("MENDER_DATABASE_URL is required when Run API is enabled")
	}
	if c.RunReadAPIEnabled {
		raw := getenv("MENDER_CURSOR_SIGNING_KEY")
		key, err := base64.RawURLEncoding.Strict().DecodeString(raw)
		if err != nil || len(raw) != 43 || base64.RawURLEncoding.EncodeToString(key) != raw {
			return APIConfig{}, errors.New("Run read API requires a 32-byte base64url cursor signing key")
		}
		if _, err = runcursor.New(key); err != nil {
			return APIConfig{}, err
		}
		c.CursorSigningKey = key
	}
	return c, nil
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// BuildAPI never migrates, seeds data or falls back to a test repository.
func BuildAPI(ctx context.Context, c APIConfig) (http.Handler, func(), error) {
	if !c.RunAPIEnabled {
		if c.RunReadAPIEnabled || c.AgentInputAPIEnabled || c.ArtifactObjectReadEnabled || c.CoordinatedCancelEnabled || c.ProviderCancelEnabled || c.ProviderCallbackEnabled || c.StartRunAPIEnabled || c.MCPGatewayEnabled || c.MCPFixedToolsetEnabled || c.ConsoleOIDCEnabled || c.ConsoleRunDelegationEnabled || c.ConsoleHumanStartEnabled || c.ConsoleExecutionRiskEnabled || c.ConsoleLaunchDiscoveryEnabled || c.ConsoleUsageEnabled || c.ConsoleConnectionsEnabled || c.ConsoleCatalogEnabled || c.AdminCatalogReviewEnabled || c.AdminCatalogPolicyEnabled || c.AdminProviderCallbacksEnabled || c.ConsoleConnectionOAuthEnabled {
			return nil, nil, errors.New("Run capabilities require the authenticated Run API")
		}
		return httpserver.NewRouter(), func() {}, nil
	}
	if c.ConsoleConnectionsEnabled && !c.ConsoleOIDCEnabled {
		return nil, nil, errors.New("Console Connections require Console OIDC")
	}
	if c.ConsoleRunDelegationEnabled && (!c.ConsoleOIDCEnabled || !c.RunReadAPIEnabled) {
		return nil, nil, errors.New("Console Run delegation requires Console OIDC and Run read API")
	}
	if c.AgentInputAPIEnabled && !c.RunReadAPIEnabled {
		return nil, nil, errors.New("Agent input API requires Run read API")
	}
	if c.ConsoleLaunchDiscoveryEnabled && !c.ConsoleOIDCEnabled {
		return nil, nil, errors.New("Console launch discovery requires Console OIDC")
	}
	if c.ConsoleUsageEnabled && !c.ConsoleOIDCEnabled {
		return nil, nil, errors.New("Console Usage requires Console OIDC")
	}
	if c.ConsoleCatalogEnabled && !c.ConsoleOIDCEnabled {
		return nil, nil, errors.New("Console Catalog requires Console OIDC")
	}
	if c.AdminCatalogReviewEnabled && (!c.ConsoleOIDCEnabled || !c.ConsoleCatalogEnabled) {
		return nil, nil, errors.New("Admin Catalog review requires Console OIDC and Console Catalog")
	}
	if c.AdminCatalogPolicyEnabled && (!c.ConsoleOIDCEnabled || !c.ConsoleCatalogEnabled) {
		return nil, nil, errors.New("Admin Catalog policy requires Console OIDC and Console Catalog")
	}
	if c.ConsoleHumanStartEnabled && (!c.ConsoleOIDCEnabled || !c.ConsoleLaunchDiscoveryEnabled || !c.StartRunAPIEnabled) {
		return nil, nil, errors.New("Console Human StartRun requires Console OIDC, launch discovery and StartRun API")
	}
	if c.ConsoleExecutionRiskEnabled && (!c.ConsoleOIDCEnabled || !c.ConsoleHumanStartEnabled || !c.ConsoleLaunchDiscoveryEnabled || !c.StartRunAPIEnabled) {
		return nil, nil, errors.New("Console execution risk requires Console OIDC, launch discovery and Human StartRun")
	}
	if c.ConsoleConnectionOAuthEnabled && (!c.ConsoleOIDCEnabled || !c.ConsoleConnectionsEnabled) {
		return nil, nil, errors.New("Connection OAuth requires Console OIDC and Console Connections")
	}
	if c.MCPGatewayEnabled && (!c.RunReadAPIEnabled || !c.StartRunAPIEnabled || !c.CoordinatedCancelEnabled) {
		return nil, nil, errors.New("MCP gateway requires Run read, StartRun and coordinated cancellation capabilities")
	}
	if c.MCPFixedToolsetEnabled && !c.MCPGatewayEnabled {
		return nil, nil, errors.New("Fixed Toolset MCP requires the authenticated MCP gateway")
	}
	var codec *runcursor.Codec
	if c.RunReadAPIEnabled {
		var err error
		codec, err = runcursor.New(c.CursorSigningKey)
		if err != nil {
			return nil, nil, err
		}
	}
	start, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, err := database.Open(start, c.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	var cancelPool *pgxpool.Pool
	var artifactObjectPool *pgxpool.Pool
	var callbackPool *pgxpool.Pool
	var admissionPool *pgxpool.Pool
	var browserSessionPool *pgxpool.Pool
	var connectionManagerPool *pgxpool.Pool
	var catalogManagerPool *pgxpool.Pool
	var publisherManagerPool *pgxpool.Pool
	var releaseManagerPool *pgxpool.Pool
	var dangerousOperationManagerPool *pgxpool.Pool
	var supportReaderPool *pgxpool.Pool
	var governanceReviewerPool *pgxpool.Pool
	var governancePolicyManagerPool *pgxpool.Pool
	var callbackObserverPool *pgxpool.Pool
	var governanceExecutionConfirmerPool *pgxpool.Pool
	var commerceObserverPool *pgxpool.Pool
	var startDelegationFacade *facade.RunStartDelegations
	var agentInputService *runapp.AgentInputSubmissions
	var closeAgentInput func()
	closePools := func() {
		if closeAgentInput != nil {
			closeAgentInput()
			closeAgentInput = nil
		}
		if artifactObjectPool != nil {
			artifactObjectPool.Close()
		}
		if callbackObserverPool != nil {
			callbackObserverPool.Close()
		}
		if callbackPool != nil {
			callbackPool.Close()
		}
		if governanceExecutionConfirmerPool != nil {
			governanceExecutionConfirmerPool.Close()
		}
		if governancePolicyManagerPool != nil {
			governancePolicyManagerPool.Close()
		}
		if governanceReviewerPool != nil {
			governanceReviewerPool.Close()
		}
		if publisherManagerPool != nil {
			publisherManagerPool.Close()
		}
		if releaseManagerPool != nil {
			releaseManagerPool.Close()
		}
		if dangerousOperationManagerPool != nil {
			dangerousOperationManagerPool.Close()
		}
		if supportReaderPool != nil {
			supportReaderPool.Close()
		}
		if catalogManagerPool != nil {
			catalogManagerPool.Close()
		}
		if commerceObserverPool != nil {
			commerceObserverPool.Close()
		}
		if connectionManagerPool != nil {
			connectionManagerPool.Close()
		}
		if browserSessionPool != nil {
			browserSessionPool.Close()
		}
		if admissionPool != nil {
			admissionPool.Close()
		}
		if cancelPool != nil {
			cancelPool.Close()
		}
		pool.Close()
	}
	failed := func(err error) (http.Handler, func(), error) { closePools(); return nil, nil, err }
	if err = migrations.Verify(start, pool); err != nil {
		return failed(err)
	}
	if err = database.RuntimeRole(start, pool); err != nil {
		return failed(err)
	}
	identity, err := identityapp.NewService(identitypg.New(pool), keycodec.Codec{}, systemClock{})
	if err != nil {
		return failed(err)
	}
	identityFacade := facade.New(identity)
	access := runidentityaccess.New(identityFacade)
	repository := runpg.New(pool)
	runs, err := runapp.NewService(repository, access, systemClock{})
	if err != nil {
		return failed(err)
	}
	if c.CoordinatedCancelEnabled {
		cancelPool, err = database.Open(start, c.CancellationDatabaseURL)
		if err != nil {
			return failed(err)
		}
		// Both connections must address the same database; never coordinate quota
		// against another instance that happens to contain matching Run identifiers.
		readerCfg, writerCfg := pool.Config().ConnConfig, cancelPool.Config().ConnConfig
		if readerCfg.Host != writerCfg.Host || readerCfg.Port != writerCfg.Port || readerCfg.Database != writerCfg.Database || readerCfg.User == writerCfg.User {
			return failed(errors.New("cancellation requires the same database with a distinct restricted role"))
		}
		cancellation, err := BuildCancellation(start, cancelPool, identitycancel.New(facade.New(identity)))
		if err != nil {
			return failed(err)
		}
		coordinator := coordinatedcancel.New(cancelfacade.New(cancellation))
		if c.ProviderCancelEnabled {
			providerRequests, buildErr := runapp.NewProviderCancelRequests(runpg.NewProviderCancelRequests(cancelPool), systemClock{})
			if buildErr != nil {
				return failed(buildErr)
			}
			runs, err = runapp.NewServiceWithCancellationAndProvider(repository, access, systemClock{}, coordinator, providerRequests)
		} else {
			runs, err = runapp.NewServiceWithCancellation(repository, access, systemClock{}, coordinator)
		}
		if err != nil {
			return failed(err)
		}
	}
	handler, err := runhttp.New(runs, access)
	if err != nil {
		return failed(err)
	}
	registers := []func(*gin.Engine){handler.Register}
	if c.AgentInputAPIEnabled {
		secrets, secretErr := filesecret.New(c.AgentInputSecretRoot)
		if secretErr != nil {
			return failed(secretErr)
		}
		agentInputService, closeAgentInput, err = BuildAgentInputRuntime(start, AgentInputRuntimeConfig{
			DatabaseURL: c.AgentInputDatabaseURL, ExecutorDatabaseURL: c.AgentInputExecutorDatabaseURL,
			ProviderIDs: c.AgentInputProviderIDs, AllowedHosts: c.AgentInputAllowedHosts,
			AllowHTTP: c.AgentInputAllowHTTP, AllowLoopback: c.AgentInputAllowLoopback,
		}, access, secrets)
		if err != nil {
			return failed(err)
		}
		agentInputHandler, inputErr := runhttp.NewAgentInput(agentInputService, access)
		if inputErr != nil {
			return failed(inputErr)
		}
		registers = append(registers, agentInputHandler.Register)
	}
	var artifactObjectRepository *runpg.Repository
	var artifactObjectStore *runobjectfs.Store
	var artifactObjectCodec *runartifactcap.Codec
	if c.ArtifactObjectReadEnabled {
		artifactObjectPool, err = database.Open(start, c.ArtifactObjectDatabaseURL)
		if err != nil {
			return failed(err)
		}
		readerCfg, objectCfg := pool.Config().ConnConfig, artifactObjectPool.Config().ConnConfig
		if readerCfg.Host != objectCfg.Host || readerCfg.Port != objectCfg.Port || readerCfg.Database != objectCfg.Database || readerCfg.User == objectCfg.User {
			return failed(errors.New("Artifact object reads require the same database with a distinct restricted role"))
		}
		if err = migrations.Verify(start, artifactObjectPool); err != nil {
			return failed(err)
		}
		if err = database.ArtifactObjectReaderRole(start, artifactObjectPool); err != nil {
			return failed(err)
		}
		artifactObjectStore, err = runobjectfs.New(c.ArtifactObjectRoot)
		if err != nil {
			return failed(err)
		}
		artifactObjectCodec, err = runartifactcap.NewFromFile(c.ArtifactObjectSigningKeyFile)
		if err != nil {
			return failed(err)
		}
		artifactObjectRepository = runpg.New(artifactObjectPool)
		machineObjectAccess, accessErr := runapp.NewArtifactObjectAccess(artifactObjectRepository, artifactObjectStore, access, artifactObjectCodec, systemClock{}, c.ArtifactObjectCapabilityTTL)
		if accessErr != nil {
			return failed(accessErr)
		}
		objectHandler, handlerErr := runhttp.NewArtifactObjects(machineObjectAccess, access)
		if handlerErr != nil {
			return failed(handlerErr)
		}
		registers = append(registers, func(router *gin.Engine) {
			objectHandler.RegisterProtectedAt(router, "/api/v1")
			objectHandler.RegisterSigned(router)
		})
	}
	if c.ProviderCallbackEnabled {
		callbackPool, err = database.Open(start, c.ProviderCallbackDatabaseURL)
		if err != nil {
			return failed(err)
		}
		readerCfg, callbackCfg := pool.Config().ConnConfig, callbackPool.Config().ConnConfig
		if readerCfg.Host != callbackCfg.Host || readerCfg.Port != callbackCfg.Port || readerCfg.Database != callbackCfg.Database || readerCfg.User == callbackCfg.User || (cancelPool != nil && callbackCfg.User == cancelPool.Config().ConnConfig.User) {
			return failed(errors.New("provider callbacks require the same database with a distinct restricted role"))
		}
		if err = migrations.Verify(start, callbackPool); err != nil {
			return failed(err)
		}
		if err = database.CallbackIngestorRole(start, callbackPool); err != nil {
			return failed(err)
		}
		callbackSecrets, buildErr := callbackfilesecret.New(c.ProviderCallbackSecretRoot, c.ProviderCallbackReviewedKeys)
		if buildErr != nil {
			return failed(buildErr)
		}
		callbackVerifier, buildErr := callbackverify.New(callbackSecrets)
		if buildErr != nil {
			return failed(buildErr)
		}
		callbackCore, buildErr := runapp.NewProviderCallbackService(runpg.NewProviderCallbacks(callbackPool))
		if buildErr != nil {
			return failed(buildErr)
		}
		callbackReceiver := callbackexecution.New(runfacade.NewProviderCallbacks(callbackCore))
		callbackService, buildErr := callbackapp.New(callbackReceiver, callbackVerifier, systemClock{})
		if buildErr != nil {
			return failed(buildErr)
		}
		callbackHandler, buildErr := callbackhttp.New(callbackService)
		if buildErr != nil {
			return failed(buildErr)
		}
		registers = append(registers, callbackHandler.Register)
	}
	if c.ConsoleOIDCEnabled {
		browserSessionPool, err = database.Open(start, c.BrowserSessionDatabaseURL)
		if err != nil {
			return failed(err)
		}
		readerCfg, sessionCfg := pool.Config().ConnConfig, browserSessionPool.Config().ConnConfig
		if readerCfg.Host != sessionCfg.Host || readerCfg.Port != sessionCfg.Port || readerCfg.Database != sessionCfg.Database || readerCfg.User == sessionCfg.User {
			return failed(errors.New("browser sessions require the same database with a distinct restricted role"))
		}
		if err = migrations.Verify(start, browserSessionPool); err != nil {
			return failed(err)
		}
		if err = database.BrowserSessionRole(start, browserSessionPool); err != nil {
			return failed(err)
		}
		clientSecret, key, readErr := loadConsoleSecrets(c.OIDCClientSecretFile, c.FlowSigningKeyFile)
		if readErr != nil {
			return failed(readErr)
		}
		oidcClient, buildErr := identityoidc.New(start, identityoidc.Config{Issuer: c.OIDCIssuer, ClientID: c.OIDCClientID, ClientSecret: clientSecret, RedirectURL: c.OIDCRedirectURL})
		if buildErr != nil {
			return failed(buildErr)
		}
		sessionTokenCodec := sessioncodec.Codec{}
		sessions, buildErr := identityapp.NewHumanSessionService(identitypg.NewHumanSessions(browserSessionPool), sessionTokenCodec, systemClock{}, c.ConsoleSessionTTL)
		if buildErr != nil {
			return failed(buildErr)
		}
		login, buildErr := identityapp.NewLoginService(oidcClient, sessions, sessionTokenCodec, systemClock{})
		if buildErr != nil {
			return failed(buildErr)
		}
		flow, buildErr := identityhttp.NewFlowCookieCodec(key)
		if buildErr != nil {
			return failed(buildErr)
		}
		consoleHandler, buildErr := identityhttp.New(login, sessions, flow, c.ConsoleCookieSecure)
		if buildErr != nil {
			return failed(buildErr)
		}
		registers = append(registers, consoleHandler.Register)
		humanIdentity := facade.NewHuman(sessions)
		if c.ConsoleUsageEnabled {
			commerceObserverPool, buildErr = database.Open(start, c.CommerceObserverDatabaseURL)
			if buildErr != nil {
				return failed(buildErr)
			}
			observerCfg := commerceObserverPool.Config().ConnConfig
			if readerCfg.Host != observerCfg.Host || readerCfg.Port != observerCfg.Port || readerCfg.Database != observerCfg.Database || observerCfg.User == readerCfg.User || observerCfg.User == sessionCfg.User {
				return failed(errors.New("Console Usage requires the same database with a distinct restricted role"))
			}
			if buildErr = migrations.Verify(start, commerceObserverPool); buildErr != nil {
				return failed(buildErr)
			}
			if buildErr = database.CommerceObserverRole(start, commerceObserverPool); buildErr != nil {
				return failed(buildErr)
			}
			usageAccess := commerceidentityaccess.NewHuman(humanIdentity)
			usageService, serviceErr := commerceapp.NewUsageService(commercepg.NewObservability(commerceObserverPool), usageAccess)
			if serviceErr != nil {
				return failed(serviceErr)
			}
			usageHandler, handlerErr := commercehttp.NewUsage(usageService, usageAccess)
			if handlerErr != nil {
				return failed(handlerErr)
			}
			registers = append(registers, usageHandler.Register)
		}
		if c.ConsoleLaunchDiscoveryEnabled {
			launchAccess := consolelaunchidentity.New(humanIdentity)
			launchService, launchErr := consolelaunchapp.New(launchAccess, consolelaunchpg.New(pool), systemClock{})
			if launchErr != nil {
				return failed(launchErr)
			}
			launchHandler, launchErr := consolelaunchhttp.New(launchService)
			if launchErr != nil {
				return failed(launchErr)
			}
			registers = append(registers, launchHandler.Register)
		}
		if c.ConsoleHumanStartEnabled {
			startDelegations, buildErr := identityapp.NewRunStartDelegationService(identitypg.NewRunStartDelegations(browserSessionPool, browserSessionPool), sessions, sessionTokenCodec, systemClock{}, c.ConsoleHumanStartDelegationTTL)
			if buildErr != nil {
				return failed(buildErr)
			}
			startDelegationFacade = facade.NewRunStartDelegations(startDelegations)
			startDelegationHandler, buildErr := identityhttp.NewRunStartDelegationHandler(sessions, startDelegations)
			if buildErr != nil {
				return failed(buildErr)
			}
			registers = append(registers, startDelegationHandler.Register)
		}
		if c.ConsoleRunDelegationEnabled {
			delegations, buildErr := identityapp.NewRunDelegationService(identitypg.NewRunDelegations(browserSessionPool, browserSessionPool), sessions, sessionTokenCodec, systemClock{}, c.ConsoleRunDelegationTTL)
			if buildErr != nil {
				return failed(buildErr)
			}
			delegationFacade := facade.NewRunDelegations(delegations)
			delegationHandler, buildErr := identityhttp.NewRunDelegationHandler(sessions, delegations)
			if buildErr != nil {
				return failed(buildErr)
			}
			registers = append(registers, delegationHandler.Register)
			if c.ConsoleUsageEnabled {
				costAccess := commerceidentityaccess.NewDelegated(delegationFacade)
				costService, costErr := commerceapp.NewRunCostService(commercepg.NewObservability(commerceObserverPool), costAccess)
				if costErr != nil {
					return failed(costErr)
				}
				costHandler, costErr := commercehttp.NewRunCost(costService, costAccess)
				if costErr != nil {
					return failed(costErr)
				}
				registers = append(registers, func(router *gin.Engine) { costHandler.RegisterAt(router, "/api/console/v1") })
			}

			delegatedAccess := runidentityaccess.NewDelegated(delegationFacade)
			if c.AgentInputAPIEnabled {
				delegatedAgentInput, inputErr := agentInputService.ForAuthorizer(delegatedAccess)
				if inputErr != nil {
					return failed(inputErr)
				}
				delegatedAgentInputHandler, inputErr := runhttp.NewAgentInput(delegatedAgentInput, delegatedAccess)
				if inputErr != nil {
					return failed(inputErr)
				}
				registers = append(registers, func(router *gin.Engine) { delegatedAgentInputHandler.RegisterAt(router, "/api/console/v1") })
			}
			if c.ArtifactObjectReadEnabled {
				delegatedObjectAccess, objectErr := runapp.NewArtifactObjectAccess(artifactObjectRepository, artifactObjectStore, delegatedAccess, artifactObjectCodec, systemClock{}, c.ArtifactObjectCapabilityTTL)
				if objectErr != nil {
					return failed(objectErr)
				}
				delegatedObjectHandler, objectErr := runhttp.NewArtifactObjects(delegatedObjectAccess, delegatedAccess)
				if objectErr != nil {
					return failed(objectErr)
				}
				registers = append(registers, func(router *gin.Engine) { delegatedObjectHandler.RegisterProtectedAt(router, "/api/console/v1") })
			}
			delegatedRuns, buildErr := runapp.NewService(repository, delegatedAccess, systemClock{})
			if buildErr != nil {
				return failed(buildErr)
			}
			if c.CoordinatedCancelEnabled {
				delegatedCancellation, cancelErr := BuildCancellation(start, cancelPool, identitycancel.NewDelegated(delegationFacade))
				if cancelErr != nil {
					return failed(cancelErr)
				}
				delegatedCoordinator := coordinatedcancel.New(cancelfacade.New(delegatedCancellation))
				if c.ProviderCancelEnabled {
					providerRequests, providerErr := runapp.NewProviderCancelRequests(runpg.NewProviderCancelRequests(cancelPool), systemClock{})
					if providerErr != nil {
						return failed(providerErr)
					}
					delegatedRuns, buildErr = runapp.NewServiceWithCancellationAndProvider(repository, delegatedAccess, systemClock{}, delegatedCoordinator, providerRequests)
				} else {
					delegatedRuns, buildErr = runapp.NewServiceWithCancellation(repository, delegatedAccess, systemClock{}, delegatedCoordinator)
				}
				if buildErr != nil {
					return failed(buildErr)
				}
			}
			delegatedRunHandler, buildErr := runhttp.New(delegatedRuns, delegatedAccess)
			if buildErr != nil {
				return failed(buildErr)
			}
			registers = append(registers, func(router *gin.Engine) { delegatedRunHandler.RegisterAt(router, "/api/console/v1") })
			delegatedQueries, queryErr := runapp.NewQueries(repository, delegatedAccess, codec, systemClock{})
			if queryErr != nil {
				return failed(queryErr)
			}
			delegatedQueryHandler, queryErr := runhttp.NewQueries(delegatedQueries, delegatedAccess)
			if queryErr != nil {
				return failed(queryErr)
			}
			registers = append(registers, func(router *gin.Engine) { delegatedQueryHandler.RegisterAt(router, "/api/console/v1") })
		}
		if c.ConsoleConnectionsEnabled {
			connectionManagerPool, buildErr = database.Open(start, c.ConnectionManagerDatabaseURL)
			if buildErr != nil {
				return failed(buildErr)
			}
			managerCfg := connectionManagerPool.Config().ConnConfig
			if readerCfg.Host != managerCfg.Host || readerCfg.Port != managerCfg.Port || readerCfg.Database != managerCfg.Database || managerCfg.User == readerCfg.User || managerCfg.User == sessionCfg.User {
				return failed(errors.New("Console Connections require the same database with a distinct restricted role"))
			}
			if buildErr = migrations.Verify(start, connectionManagerPool); buildErr != nil {
				return failed(buildErr)
			}
			if buildErr = database.ConnectionManagerRole(start, connectionManagerPool); buildErr != nil {
				return failed(buildErr)
			}
			humanAccess := connectionidentityaccess.NewHuman(humanIdentity)
			connectionService, serviceErr := connectionapp.NewHuman(connectionpg.New(connectionManagerPool), humanAccess)
			if serviceErr != nil {
				return failed(serviceErr)
			}
			connectionHandler, handlerErr := connectionhttp.New(connectionService, humanAccess)
			if handlerErr != nil {
				return failed(handlerErr)
			}
			registers = append(registers, connectionHandler.Register)
			if c.ConsoleConnectionOAuthEnabled {
				oauthClientSecret, secretErr := loadMountedTextSecret(c.ConnectionOAuthClientSecretFile, "Connection OAuth client secret")
				if secretErr != nil {
					return failed(secretErr)
				}
				oauthProvider, providerErr := connectionoauth.New(connectionoauth.Config{
					ProviderID: c.ConnectionOAuthProviderID, AuthorizationURL: c.ConnectionOAuthAuthorizationURL,
					TokenURL: c.ConnectionOAuthTokenURL, ClientID: c.ConnectionOAuthClientID, ClientSecret: oauthClientSecret,
					RedirectURL: c.ConnectionOAuthRedirectURL, Scopes: c.ConnectionOAuthScopes,
				}, nil)
				if providerErr != nil {
					return failed(providerErr)
				}
				vault, vaultErr := filesecret.New(c.ConnectionOAuthSecretRoot)
				if vaultErr != nil {
					return failed(vaultErr)
				}
				credentialStore := connectionsupplycredentials.New(vault)
				var oauthService *connectionapp.OAuthService
				var oauthErr error
				if c.ConnectionOAuthRefreshEnabled {
					oauthService, oauthErr = connectionapp.NewRefreshableOAuth(humanAccess, connectionpg.New(connectionManagerPool), oauthProvider, credentialStore, connectionrandom.Generator{}, systemClock{}, c.ConnectionOAuthFlowTTL, c.ConnectionOAuthScopes)
				} else {
					oauthService, oauthErr = connectionapp.NewOAuth(humanAccess, connectionpg.New(connectionManagerPool), oauthProvider, credentialStore, connectionrandom.Generator{}, systemClock{}, c.ConnectionOAuthFlowTTL)
				}
				if oauthErr != nil {
					return failed(oauthErr)
				}
				oauthFlow, oauthErr := connectionhttp.NewOAuthFlowCookieCodec(key)
				if oauthErr != nil {
					return failed(oauthErr)
				}
				oauthHandler, oauthErr := connectionhttp.NewOAuth(oauthService, humanAccess, oauthFlow, c.ConsoleCookieSecure)
				if oauthErr != nil {
					return failed(oauthErr)
				}
				registers = append(registers, oauthHandler.Register)
			}
		}
		if c.ConsoleCatalogEnabled {
			catalogManagerPool, buildErr = database.Open(start, c.CatalogManagerDatabaseURL)
			if buildErr != nil {
				return failed(buildErr)
			}
			catalogCfg := catalogManagerPool.Config().ConnConfig
			if readerCfg.Host != catalogCfg.Host || readerCfg.Port != catalogCfg.Port || readerCfg.Database != catalogCfg.Database || catalogCfg.User == readerCfg.User || catalogCfg.User == sessionCfg.User || (connectionManagerPool != nil && catalogCfg.User == connectionManagerPool.Config().ConnConfig.User) || (commerceObserverPool != nil && catalogCfg.User == commerceObserverPool.Config().ConnConfig.User) {
				return failed(errors.New("Console Catalog requires the same database with a distinct restricted role"))
			}
			if buildErr = migrations.Verify(start, catalogManagerPool); buildErr != nil {
				return failed(buildErr)
			}
			if buildErr = database.CatalogManagerRole(start, catalogManagerPool); buildErr != nil {
				return failed(buildErr)
			}
			catalogAccess := catalogmanagementidentity.New(humanIdentity)
			catalogService, serviceErr := catalogmanagementapp.New(catalogmanagementpg.New(catalogManagerPool), catalogAccess, systemClock{}, catalogmanagementrandom.Generator{})
			if serviceErr != nil {
				return failed(serviceErr)
			}
			catalogHandler, handlerErr := catalogmanagementhttp.New(catalogService, catalogAccess)
			if handlerErr != nil {
				return failed(handlerErr)
			}
			registers = append(registers, catalogHandler.Register)
			openAPIImportService, serviceErr := catalogmanagementapp.NewOpenAPIImport(catalogService, catalogmanagementopenapi.New(openapifacade.New()))
			if serviceErr != nil {
				return failed(serviceErr)
			}
			openAPIImportHandler, handlerErr := catalogmanagementhttp.NewOpenAPIImport(openAPIImportService, catalogAccess)
			if handlerErr != nil {
				return failed(handlerErr)
			}
			registers = append(registers, openAPIImportHandler.Register)
		}
		if c.ConsolePublisherEnabled {
			publisherManagerPool, buildErr = database.Open(start, c.PublisherManagerDatabaseURL)
			if buildErr != nil {
				return failed(buildErr)
			}
			publisherCfg := publisherManagerPool.Config().ConnConfig
			if readerCfg.Host != publisherCfg.Host || readerCfg.Port != publisherCfg.Port || readerCfg.Database != publisherCfg.Database || publisherCfg.User == readerCfg.User || publisherCfg.User == sessionCfg.User || (catalogManagerPool != nil && publisherCfg.User == catalogManagerPool.Config().ConnConfig.User) || (connectionManagerPool != nil && publisherCfg.User == connectionManagerPool.Config().ConnConfig.User) || (commerceObserverPool != nil && publisherCfg.User == commerceObserverPool.Config().ConnConfig.User) {
				return failed(errors.New("Console Publisher requires the same database with a distinct restricted role"))
			}
			if buildErr = migrations.Verify(start, publisherManagerPool); buildErr != nil {
				return failed(buildErr)
			}
			if buildErr = database.PublisherManagerRole(start, publisherManagerPool); buildErr != nil {
				return failed(buildErr)
			}
			publisherAccess := supplyidentity.NewHuman(humanIdentity)
			publisherRepository := supplypg.NewPublicationRepository(publisherManagerPool)
			publisherService, serviceErr := supplyapp.NewPublisherService(publisherRepository, publisherAccess, supplymanifesthash.New())
			if serviceErr != nil {
				return failed(serviceErr)
			}
			publisherHandler, handlerErr := supplyhttp.NewPublication(publisherService, publisherAccess)
			if handlerErr != nil {
				return failed(handlerErr)
			}
			registers = append(registers, publisherHandler.Register)
			publisherWorkflow, serviceErr := supplyapp.NewPublicationWorkflow(publisherRepository, publisherAccess, supplyrandom.PublicationIDs{}, systemClock{})
			if serviceErr != nil {
				return failed(serviceErr)
			}
			workflowHandler, handlerErr := supplyhttp.NewPublicationWorkflow(publisherWorkflow, publisherAccess)
			if handlerErr != nil {
				return failed(handlerErr)
			}
			registers = append(registers, workflowHandler.Register)
		}
		if c.AdminReleaseGovernanceEnabled {
			releaseManagerPool, buildErr = database.Open(start, c.ReleaseManagerDatabaseURL)
			if buildErr != nil {
				return failed(buildErr)
			}
			releaseCfg := releaseManagerPool.Config().ConnConfig
			if readerCfg.Host != releaseCfg.Host || readerCfg.Port != releaseCfg.Port || readerCfg.Database != releaseCfg.Database || releaseCfg.User == readerCfg.User || releaseCfg.User == sessionCfg.User ||
				(catalogManagerPool != nil && releaseCfg.User == catalogManagerPool.Config().ConnConfig.User) || (publisherManagerPool != nil && releaseCfg.User == publisherManagerPool.Config().ConnConfig.User) ||
				(connectionManagerPool != nil && releaseCfg.User == connectionManagerPool.Config().ConnConfig.User) || (commerceObserverPool != nil && releaseCfg.User == commerceObserverPool.Config().ConnConfig.User) {
				return failed(errors.New("Admin Release Governance requires the same database with a distinct restricted role"))
			}
			if buildErr = migrations.Verify(start, releaseManagerPool); buildErr != nil {
				return failed(buildErr)
			}
			if buildErr = database.ReleaseManagerRole(start, releaseManagerPool); buildErr != nil {
				return failed(buildErr)
			}
			releaseAccess := supplyidentity.NewHuman(humanIdentity)
			releaseRepository := supplypg.NewReleaseGovernanceRepository(releaseManagerPool)
			releaseService, serviceErr := supplyapp.NewReleaseGovernance(releaseRepository, releaseAccess, supplyrandom.ReleaseIDs{}, systemClock{})
			if serviceErr != nil {
				return failed(serviceErr)
			}
			releaseHandler, handlerErr := supplyhttp.NewReleaseGovernance(releaseService, releaseAccess)
			if handlerErr != nil {
				return failed(handlerErr)
			}
			registers = append(registers, releaseHandler.Register)
		}
		if c.AdminDangerousOperationEnabled {
			dangerousOperationManagerPool, buildErr = database.Open(start, c.DangerousOperationManagerDatabaseURL)
			if buildErr != nil {
				return failed(buildErr)
			}
			dangerousCfg := dangerousOperationManagerPool.Config().ConnConfig
			if readerCfg.Host != dangerousCfg.Host || readerCfg.Port != dangerousCfg.Port || readerCfg.Database != dangerousCfg.Database || dangerousCfg.User == readerCfg.User || dangerousCfg.User == sessionCfg.User ||
				(catalogManagerPool != nil && dangerousCfg.User == catalogManagerPool.Config().ConnConfig.User) || (publisherManagerPool != nil && dangerousCfg.User == publisherManagerPool.Config().ConnConfig.User) ||
				(releaseManagerPool != nil && dangerousCfg.User == releaseManagerPool.Config().ConnConfig.User) || (connectionManagerPool != nil && dangerousCfg.User == connectionManagerPool.Config().ConnConfig.User) ||
				(commerceObserverPool != nil && dangerousCfg.User == commerceObserverPool.Config().ConnConfig.User) {
				return failed(errors.New("Admin Dangerous Operation requires the same database with a distinct restricted role"))
			}
			if buildErr = migrations.Verify(start, dangerousOperationManagerPool); buildErr != nil {
				return failed(buildErr)
			}
			if buildErr = database.DangerousOperationManagerRole(start, dangerousOperationManagerPool); buildErr != nil {
				return failed(buildErr)
			}
			dangerousAccess := governanceidentity.New(humanIdentity)
			dangerousService, serviceErr := governanceapp.NewDangerousOperationService(governancepg.NewDangerousOperation(dangerousOperationManagerPool), dangerousAccess, governancerandom.DangerousOperationIDs{}, systemClock{})
			if serviceErr != nil {
				return failed(serviceErr)
			}
			dangerousHandler, handlerErr := governancehttp.NewDangerousOperation(dangerousService, dangerousAccess)
			if handlerErr != nil {
				return failed(handlerErr)
			}
			registers = append(registers, dangerousHandler.Register)
		}
		if c.AdminSupportAccessEnabled {
			supportReaderPool, buildErr = database.Open(start, c.SupportReaderDatabaseURL)
			if buildErr != nil {
				return failed(buildErr)
			}
			supportCfg := supportReaderPool.Config().ConnConfig
			if dangerousOperationManagerPool == nil || readerCfg.Host != supportCfg.Host || readerCfg.Port != supportCfg.Port || readerCfg.Database != supportCfg.Database ||
				supportCfg.User == readerCfg.User || supportCfg.User == sessionCfg.User || supportCfg.User == dangerousOperationManagerPool.Config().ConnConfig.User ||
				(releaseManagerPool != nil && supportCfg.User == releaseManagerPool.Config().ConnConfig.User) || (catalogManagerPool != nil && supportCfg.User == catalogManagerPool.Config().ConnConfig.User) ||
				(publisherManagerPool != nil && supportCfg.User == publisherManagerPool.Config().ConnConfig.User) || (connectionManagerPool != nil && supportCfg.User == connectionManagerPool.Config().ConnConfig.User) ||
				(commerceObserverPool != nil && supportCfg.User == commerceObserverPool.Config().ConnConfig.User) {
				return failed(errors.New("Admin Support Access requires the same database with a distinct restricted role"))
			}
			if buildErr = migrations.Verify(start, supportReaderPool); buildErr != nil {
				return failed(buildErr)
			}
			if buildErr = database.SupportReaderRole(start, supportReaderPool); buildErr != nil {
				return failed(buildErr)
			}
			supportAccess := governanceidentity.New(humanIdentity)
			supportService, serviceErr := governanceapp.NewSupportAccessService(
				governancepg.NewDangerousOperation(dangerousOperationManagerPool), governancepg.NewSupportRead(supportReaderPool),
				supportAccess, governancerandom.DangerousOperationIDs{}, systemClock{},
			)
			if serviceErr != nil {
				return failed(serviceErr)
			}
			supportHandler, handlerErr := governancehttp.NewSupportAccess(supportService, supportAccess)
			if handlerErr != nil {
				return failed(handlerErr)
			}
			registers = append(registers, supportHandler.Register)
		}
		if c.AdminCatalogReviewEnabled || c.AdminPluginReviewEnabled {
			governanceReviewerPool, buildErr = database.Open(start, c.GovernanceReviewerDatabaseURL)
			if buildErr != nil {
				return failed(buildErr)
			}
			reviewerCfg := governanceReviewerPool.Config().ConnConfig
			if readerCfg.Host != reviewerCfg.Host || readerCfg.Port != reviewerCfg.Port || readerCfg.Database != reviewerCfg.Database || reviewerCfg.User == readerCfg.User || reviewerCfg.User == sessionCfg.User || (catalogManagerPool != nil && reviewerCfg.User == catalogManagerPool.Config().ConnConfig.User) || (publisherManagerPool != nil && reviewerCfg.User == publisherManagerPool.Config().ConnConfig.User) || (releaseManagerPool != nil && reviewerCfg.User == releaseManagerPool.Config().ConnConfig.User) || (connectionManagerPool != nil && reviewerCfg.User == connectionManagerPool.Config().ConnConfig.User) || (commerceObserverPool != nil && reviewerCfg.User == commerceObserverPool.Config().ConnConfig.User) {
				return failed(errors.New("Admin publication review requires the same database with a distinct restricted role"))
			}
			if buildErr = migrations.Verify(start, governanceReviewerPool); buildErr != nil {
				return failed(buildErr)
			}
			if buildErr = database.GovernanceReviewerRole(start, governanceReviewerPool); buildErr != nil {
				return failed(buildErr)
			}
			reviewAccess := governanceidentity.New(humanIdentity)
			reviewRepository := governancepg.NewPublicationReview(governanceReviewerPool)
			if c.AdminCatalogReviewEnabled {
				reviewService, serviceErr := governanceapp.New(reviewRepository, reviewAccess, systemClock{})
				if serviceErr != nil {
					return failed(serviceErr)
				}
				reviewHandler, handlerErr := governancehttp.NewPublicationReview(reviewService, reviewAccess)
				if handlerErr != nil {
					return failed(handlerErr)
				}
				registers = append(registers, reviewHandler.Register)
				historyService, serviceErr := governanceapp.NewHistory(reviewRepository, reviewAccess)
				if serviceErr != nil {
					return failed(serviceErr)
				}
				historyHandler, handlerErr := governancehttp.NewPublicationHistory(historyService, reviewAccess)
				if handlerErr != nil {
					return failed(handlerErr)
				}
				registers = append(registers, historyHandler.Register)
			}
			if c.AdminPluginReviewEnabled {
				pluginReviewService, serviceErr := governanceapp.NewPluginPublicationReview(reviewRepository, reviewAccess, systemClock{})
				if serviceErr != nil {
					return failed(serviceErr)
				}
				pluginReviewHandler, handlerErr := governancehttp.NewPluginPublicationReview(pluginReviewService, reviewAccess)
				if handlerErr != nil {
					return failed(handlerErr)
				}
				registers = append(registers, pluginReviewHandler.Register)
			}
		}
		if c.AdminCatalogPolicyEnabled {
			governancePolicyManagerPool, buildErr = database.Open(start, c.GovernancePolicyManagerDatabaseURL)
			if buildErr != nil {
				return failed(buildErr)
			}
			policyCfg := governancePolicyManagerPool.Config().ConnConfig
			if readerCfg.Host != policyCfg.Host || readerCfg.Port != policyCfg.Port || readerCfg.Database != policyCfg.Database || policyCfg.User == readerCfg.User || policyCfg.User == sessionCfg.User || (catalogManagerPool != nil && policyCfg.User == catalogManagerPool.Config().ConnConfig.User) || (releaseManagerPool != nil && policyCfg.User == releaseManagerPool.Config().ConnConfig.User) || (governanceReviewerPool != nil && policyCfg.User == governanceReviewerPool.Config().ConnConfig.User) || (connectionManagerPool != nil && policyCfg.User == connectionManagerPool.Config().ConnConfig.User) || (commerceObserverPool != nil && policyCfg.User == commerceObserverPool.Config().ConnConfig.User) {
				return failed(errors.New("Admin Catalog policy requires the same database with a distinct restricted role"))
			}
			if buildErr = migrations.Verify(start, governancePolicyManagerPool); buildErr != nil {
				return failed(buildErr)
			}
			if buildErr = database.GovernancePolicyManagerRole(start, governancePolicyManagerPool); buildErr != nil {
				return failed(buildErr)
			}
			policyAccess := governanceidentity.New(humanIdentity)
			policyService, serviceErr := governanceapp.NewPolicy(governancepg.NewPublicationPolicy(governancePolicyManagerPool), policyAccess, systemClock{})
			if serviceErr != nil {
				return failed(serviceErr)
			}
			policyHandler, handlerErr := governancehttp.NewPublicationPolicy(policyService, policyAccess)
			if handlerErr != nil {
				return failed(handlerErr)
			}
			registers = append(registers, policyHandler.Register)
			executionGovernanceService, serviceErr := governanceapp.NewExecutionGovernance(governancepg.NewExecutionGovernance(governancePolicyManagerPool), policyAccess, systemClock{})
			if serviceErr != nil {
				return failed(serviceErr)
			}
			executionGovernanceHandler, handlerErr := governancehttp.NewExecutionGovernance(executionGovernanceService, policyAccess)
			if handlerErr != nil {
				return failed(handlerErr)
			}
			registers = append(registers, executionGovernanceHandler.Register)
		}
		if c.AdminProviderCallbacksEnabled {
			callbackObserverPool, buildErr = database.Open(start, c.CallbackObserverDatabaseURL)
			if buildErr != nil {
				return failed(buildErr)
			}
			observerCfg := callbackObserverPool.Config().ConnConfig
			if readerCfg.Host != observerCfg.Host || readerCfg.Port != observerCfg.Port || readerCfg.Database != observerCfg.Database || observerCfg.User == readerCfg.User || observerCfg.User == sessionCfg.User ||
				(callbackPool != nil && observerCfg.User == callbackPool.Config().ConnConfig.User) || (catalogManagerPool != nil && observerCfg.User == catalogManagerPool.Config().ConnConfig.User) ||
				(governanceReviewerPool != nil && observerCfg.User == governanceReviewerPool.Config().ConnConfig.User) || (governancePolicyManagerPool != nil && observerCfg.User == governancePolicyManagerPool.Config().ConnConfig.User) ||
				(connectionManagerPool != nil && observerCfg.User == connectionManagerPool.Config().ConnConfig.User) || (commerceObserverPool != nil && observerCfg.User == commerceObserverPool.Config().ConnConfig.User) {
				return failed(errors.New("Admin Provider callbacks require the same database with a distinct restricted role"))
			}
			if buildErr = migrations.Verify(start, callbackObserverPool); buildErr != nil {
				return failed(buildErr)
			}
			if buildErr = database.CallbackObserverRole(start, callbackObserverPool); buildErr != nil {
				return failed(buildErr)
			}
			callbackAccess := governanceidentity.New(humanIdentity)
			callbackService, serviceErr := governanceapp.NewProviderCallbackInbox(governancepg.NewProviderCallbackInbox(callbackObserverPool), callbackAccess)
			if serviceErr != nil {
				return failed(serviceErr)
			}
			callbackHandler, handlerErr := governancehttp.NewProviderCallbackInbox(callbackService, callbackAccess)
			if handlerErr != nil {
				return failed(handlerErr)
			}
			registers = append(registers, callbackHandler.Register)
		}
		if c.ConsoleExecutionRiskEnabled {
			governanceExecutionConfirmerPool, buildErr = database.Open(start, c.GovernanceExecutionConfirmerDatabaseURL)
			if buildErr != nil {
				return failed(buildErr)
			}
			confirmerCfg := governanceExecutionConfirmerPool.Config().ConnConfig
			if readerCfg.Host != confirmerCfg.Host || readerCfg.Port != confirmerCfg.Port || readerCfg.Database != confirmerCfg.Database || confirmerCfg.User == readerCfg.User || confirmerCfg.User == sessionCfg.User || (catalogManagerPool != nil && confirmerCfg.User == catalogManagerPool.Config().ConnConfig.User) || (governanceReviewerPool != nil && confirmerCfg.User == governanceReviewerPool.Config().ConnConfig.User) || (governancePolicyManagerPool != nil && confirmerCfg.User == governancePolicyManagerPool.Config().ConnConfig.User) || (connectionManagerPool != nil && confirmerCfg.User == connectionManagerPool.Config().ConnConfig.User) || (commerceObserverPool != nil && confirmerCfg.User == commerceObserverPool.Config().ConnConfig.User) {
				return failed(errors.New("Console execution risk requires the same database with a distinct restricted role"))
			}
			if buildErr = migrations.Verify(start, governanceExecutionConfirmerPool); buildErr != nil {
				return failed(buildErr)
			}
			if buildErr = database.GovernanceExecutionConfirmerRole(start, governanceExecutionConfirmerPool); buildErr != nil {
				return failed(buildErr)
			}
			executionRiskAccess := governanceidentity.New(humanIdentity)
			executionRiskCore, serviceErr := governanceapp.NewExecutionRiskCore(governancepg.NewExecutionRiskPool(governanceExecutionConfirmerPool))
			if serviceErr != nil {
				return failed(serviceErr)
			}
			executionRiskService, serviceErr := governanceapp.NewExecutionRiskHuman(executionRiskCore, executionRiskAccess, systemClock{}, governancerandom.ConfirmationIDs{})
			if serviceErr != nil {
				return failed(serviceErr)
			}
			executionRiskHandler, handlerErr := governancehttp.NewExecutionRisk(executionRiskService, executionRiskAccess)
			if handlerErr != nil {
				return failed(handlerErr)
			}
			registers = append(registers, executionRiskHandler.Register)
		}
	}
	var queries *runapp.Queries
	if c.RunReadAPIEnabled {
		queries, err = runapp.NewQueries(repository, access, codec, systemClock{})
		if err != nil {
			return failed(err)
		}
		readHandler, err := runhttp.NewQueries(queries, access)
		if err != nil {
			return failed(err)
		}
		registers = append(registers, readHandler.Register)
	}
	var admission *admissionapp.Service
	if c.StartRunAPIEnabled {
		if c.AdmissionDatabaseURL == "" {
			return failed(errors.New("StartRun API requires a separate admission database role"))
		}
		admissionPool, err = database.Open(start, c.AdmissionDatabaseURL)
		if err != nil {
			return failed(err)
		}
		readerCfg, writerCfg := pool.Config().ConnConfig, admissionPool.Config().ConnConfig
		if readerCfg.Host != writerCfg.Host || readerCfg.Port != writerCfg.Port || readerCfg.Database != writerCfg.Database || readerCfg.User == writerCfg.User || (governanceExecutionConfirmerPool != nil && writerCfg.User == governanceExecutionConfirmerPool.Config().ConnConfig.User) {
			return failed(errors.New("admission requires the same database with a distinct restricted role"))
		}
		resolver, err := BuildAdmissionResolver(pool)
		if err != nil {
			return failed(err)
		}
		startAccess := admissionidentityaccess.New(identityFacade)
		admission, err = BuildAdmission(start, admissionPool, startAccess, resolver)
		if err != nil {
			return failed(err)
		}
		startHandler, err := admissionhttp.New(admission, startAccess)
		if err != nil {
			return failed(err)
		}
		registers = append(registers, startHandler.Register)
		if c.ConsoleHumanStartEnabled {
			if startDelegationFacade == nil {
				return failed(errors.New("Console Human StartRun delegation is not configured"))
			}
			humanStartAccess := admissionidentitystart.New(startDelegationFacade)
			humanAdmission, buildErr := BuildAdmission(start, admissionPool, humanStartAccess, resolver)
			if buildErr != nil {
				return failed(buildErr)
			}
			humanStartHandler, buildErr := admissionhttp.New(humanAdmission, humanStartAccess)
			if buildErr != nil {
				return failed(buildErr)
			}
			registers = append(registers, func(router *gin.Engine) { humanStartHandler.RegisterAt(router, "/api/console/v1") })
		}
	}
	if c.MCPGatewayEnabled {
		bridge, buildErr := mcpapp.New(mcpidentity.New(identityFacade), mcpadmission.New(admissionfacade.NewAdmission(admission)), mcpexecution.New(runfacade.NewRuns(runs, queries)))
		if buildErr != nil {
			return failed(buildErr)
		}
		mcpHandler, buildErr := mcphttp.New(bridge)
		if buildErr != nil {
			return failed(buildErr)
		}
		registers = append(registers, func(router *gin.Engine) {
			router.Any("/mcp/v1/workspaces/:workspace_id", gin.WrapH(mcpHandler))
		})
		if c.MCPFixedToolsetEnabled {
			registry, buildErr := BuildFixedToolRegistry(pool)
			if buildErr != nil {
				return failed(buildErr)
			}
			fixedIdentity := mcpidentity.New(identityFacade)
			fixedBridge, buildErr := mcpapp.NewFixed(fixedIdentity, fixedIdentity, mcpadmission.New(admissionfacade.NewAdmission(admission)), registry)
			if buildErr != nil {
				return failed(buildErr)
			}
			fixedHandler, buildErr := mcphttp.NewFixed(fixedBridge)
			if buildErr != nil {
				return failed(buildErr)
			}
			registers = append(registers, func(router *gin.Engine) {
				router.Any("/mcp/v1/workspaces/:workspace_id/toolsets/:toolset_version_id", gin.WrapH(fixedHandler))
			})
		}
	}
	register := func(router *gin.Engine) {
		for _, register := range registers {
			register(router)
		}
	}
	ready := func(ctx context.Context) error {
		if err := migrations.Verify(ctx, pool); err != nil {
			return err
		}
		if err := database.RuntimeRole(ctx, pool); err != nil {
			return err
		}
		if cancelPool != nil {
			if err := migrations.Verify(ctx, cancelPool); err != nil {
				return err
			}
			if err := database.CancellationRole(ctx, cancelPool); err != nil {
				return err
			}
		}
		if callbackPool != nil {
			if err := migrations.Verify(ctx, callbackPool); err != nil {
				return err
			}
			if err := database.CallbackIngestorRole(ctx, callbackPool); err != nil {
				return err
			}
		}
		if callbackObserverPool != nil {
			if err := migrations.Verify(ctx, callbackObserverPool); err != nil {
				return err
			}
			if err := database.CallbackObserverRole(ctx, callbackObserverPool); err != nil {
				return err
			}
		}
		if admissionPool != nil {
			if err := migrations.Verify(ctx, admissionPool); err != nil {
				return err
			}
			if err := database.AdmissionRole(ctx, admissionPool); err != nil {
				return err
			}
		}
		if browserSessionPool != nil {
			if err := migrations.Verify(ctx, browserSessionPool); err != nil {
				return err
			}
			if err := database.BrowserSessionRole(ctx, browserSessionPool); err != nil {
				return err
			}
		}
		if connectionManagerPool != nil {
			if err := migrations.Verify(ctx, connectionManagerPool); err != nil {
				return err
			}
			if err := database.ConnectionManagerRole(ctx, connectionManagerPool); err != nil {
				return err
			}
		}
		if catalogManagerPool != nil {
			if err := migrations.Verify(ctx, catalogManagerPool); err != nil {
				return err
			}
			if err := database.CatalogManagerRole(ctx, catalogManagerPool); err != nil {
				return err
			}
		}
		if publisherManagerPool != nil {
			if err := migrations.Verify(ctx, publisherManagerPool); err != nil {
				return err
			}
			if err := database.PublisherManagerRole(ctx, publisherManagerPool); err != nil {
				return err
			}
		}
		if releaseManagerPool != nil {
			if err := migrations.Verify(ctx, releaseManagerPool); err != nil {
				return err
			}
			if err := database.ReleaseManagerRole(ctx, releaseManagerPool); err != nil {
				return err
			}
		}
		if dangerousOperationManagerPool != nil {
			if err := migrations.Verify(ctx, dangerousOperationManagerPool); err != nil {
				return err
			}
			if err := database.DangerousOperationManagerRole(ctx, dangerousOperationManagerPool); err != nil {
				return err
			}
		}
		if supportReaderPool != nil {
			if err := migrations.Verify(ctx, supportReaderPool); err != nil {
				return err
			}
			if err := database.SupportReaderRole(ctx, supportReaderPool); err != nil {
				return err
			}
		}
		if governanceReviewerPool != nil {
			if err := migrations.Verify(ctx, governanceReviewerPool); err != nil {
				return err
			}
			if err := database.GovernanceReviewerRole(ctx, governanceReviewerPool); err != nil {
				return err
			}
		}
		if governancePolicyManagerPool != nil {
			if err := migrations.Verify(ctx, governancePolicyManagerPool); err != nil {
				return err
			}
			if err := database.GovernancePolicyManagerRole(ctx, governancePolicyManagerPool); err != nil {
				return err
			}
		}
		if governanceExecutionConfirmerPool != nil {
			if err := migrations.Verify(ctx, governanceExecutionConfirmerPool); err != nil {
				return err
			}
			if err := database.GovernanceExecutionConfirmerRole(ctx, governanceExecutionConfirmerPool); err != nil {
				return err
			}
		}
		if commerceObserverPool != nil {
			if err := migrations.Verify(ctx, commerceObserverPool); err != nil {
				return err
			}
			if err := database.CommerceObserverRole(ctx, commerceObserverPool); err != nil {
				return err
			}
		}
		return nil
	}
	return httpserver.NewConfiguredRouter(ready, register), closePools, nil
}
