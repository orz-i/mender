# Reviewed OAuth Connection Alpha · 2026-09-12

Mender now supports one explicitly reviewed OAuth Connection provider per API process. The browser starts authorization, but provider endpoints, scopes, client credentials and credential storage are all server-owned configuration.

## Security contract

- `MENDER_CONSOLE_CONNECTION_OAUTH_ENABLED=true` is fail-closed and requires Console OIDC plus the restricted Connection manager role.
- The browser sends only Workspace context and CSRF. It cannot select Provider ID, authorization/token endpoint, OAuth scope, client ID, redirect URI, credential version, Connection ID or storage location.
- Authorization Code + PKCE S256 is mandatory. Flow state/verifier/User/Workspace/Provider/expiry are kept in a signed HttpOnly SameSite=Lax flow cookie; the callback re-authenticates the human session and re-authorizes current `connection:manage` membership.
- Provider authorization/token endpoints must be HTTPS on the default TLS port, cannot use localhost, literal IPs or `.local`, and the reviewed token client disables redirects. The default token egress transport resolves DNS and rejects loopback/private/link-local/multicast/unspecified addresses before dialing.
- The provider access credential is never returned to the browser, URL, Connection DTO or log. The API writes it through Supply's `CredentialVault`; the mounted file adapter uses the same deterministic Connection credential address as runtime SecretProvider, `O_EXCL` immutable creation and owner-only file permissions.
- Connection metadata and the user's initial Connection Grant are written through the restricted Connection manager role under Workspace RLS. That role may insert `credential_version_ref` but still cannot select it or read Connection grants after creation.
- If credential storage succeeds but the database transaction fails, the application attempts server-side credential deletion without inheriting caller cancellation. An unreferenced file can still require operator reconciliation if that compensation itself fails; this Alpha does not claim distributed atomicity.

## HTTP flow

```text
POST /api/console/v1/workspaces/{workspace_id}/connections/oauth/start
  Cookie: HttpOnly mender_session
  X-Mender-CSRF: <browser-readable CSRF token>

GET /api/console/v1/connections/oauth/callback?state=...&code=...
  Cookie: HttpOnly mender_session + signed OAuth flow cookie
```

Start returns only `provider_id` and `authorization_url`. Successful callback redirects to `/connections?workspace=...&oauth=connected`; provider denial redirects with `oauth=denied`. Neither redirect contains provider credential material.

## Connection state

Successful exchange creates:

- an `active` Connection at revision 1;
- an opaque credential-version reference known only to server-side storage/runtime paths;
- one active Connection Grant for the authenticated human User ID;
- expiry bounded by the reviewed provider access credential expiry.

The current Alpha accepts Bearer access credentials with explicit expiry up to 24 hours and intentionally ignores refresh credentials. Long-lived refresh/rotation semantics need a separate reviewed lifecycle before production use.

## Console

Owner/Admin users see **连接 OAuth** on `/connections`. The Console calls only the same-origin start endpoint and then navigates to the reviewed HTTPS authorization URL returned by the server. It never receives the provider access credential. Viewer membership remains read-only.

## Not delivered

BYOK secret ingestion, multiple dynamically configured OAuth providers, refresh-token rotation, provider account introspection, Connection Grant editing, secret deletion on normal revoke, and production credential reconciliation are not included. Revoking a Connection blocks runtime use through Connection state/authorization but does not yet erase the stored provider credential.
