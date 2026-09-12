# Human browser session / OIDC foundation · 2026-09-12

Mender now has an explicitly enabled human Console authentication foundation. It is separate from machine API Keys and remains fail-closed unless `MENDER_CONSOLE_OIDC_ENABLED=true` is combined with the authenticated Run API and a dedicated browser-session database role.

## Implemented

- Migration `0021_human_browser_sessions.sql`: human users, Workspace memberships, reviewed OIDC subject links and opaque browser sessions.
- Browser session cookie uses a 32-byte random opaque token; PostgreSQL stores only SHA-256 digest. A separate CSRF random value is also stored only as a digest.
- `mender_session` is HttpOnly, same-origin, SameSite=Lax and Secure by default. `mender_csrf` is readable only so Console JavaScript can echo it in `X-Mender-CSRF` for state-changing requests. Logout rejects missing/mismatched CSRF.
- OIDC Authorization Code flow uses discovery, ID-token verification, nonce, state and PKCE S256. The client secret and login-flow HMAC key are mounted files, not browser configuration or ordinary business rows.
- `/auth/login`, `/auth/callback`, `/api/console/v1/session`, `/api/console/v1/workspaces` and session logout are registered only when reviewed Console OIDC configuration is enabled.
- Dedicated `grant-browser-session` role can read human identity/session facts and create/revoke browser sessions, but cannot read machine API Keys, Execution, Commerce, Connections or Supply.
- OIDC subject links are pre-provisioned; successful sign-in never creates an arbitrary Workspace or escalates membership.

## Verification

- Focused HTTP tests exercise state/callback, HttpOnly session cookie, readable CSRF cookie, authenticated session read and CSRF-protected logout.
- Real isolated PostgreSQL verifies role isolation, OIDC subject resolution, opaque session issuance, membership revalidation and revocation.

## Not claimed

This foundation does not yet provide public user registration, invitation flows, automatic OIDC user provisioning, Workspace creation, Connection management, MFA policy, support impersonation or production IdP certification. Human membership remains operator-seeded until the next slices add the self-service surface.
