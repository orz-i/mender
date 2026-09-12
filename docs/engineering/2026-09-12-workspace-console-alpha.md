# Workspace Console Alpha · 2026-09-12

Mender Console now has a first human self-service entry built on the OIDC browser-session foundation.

## Delivered

- `pnpm human:provision -- --workspace ... --user ... --issuer https://... --oidc-subject ... --membership-role ...` is an operator-only, idempotent provisioning path. It creates a reviewed human user/OIDC subject link and Workspace membership but does not expose public registration.
- `@mender/api-client` adds a strict Console identity client for `/api/console/v1/session` and `/api/console/v1/workspaces`. It uses browser-managed same-origin cookies and never adds an Authorization bearer header.
- Console `/workspaces` displays the authenticated human user and active Workspace memberships. The domain/application layers remain pure TypeScript; only infrastructure reads the non-HttpOnly CSRF cookie for logout.
- Workspace cards can open Run Explorer with a non-secret `workspace` query parameter. The current Run API is still machine-key authenticated, so the OIDC session does **not** silently become a Run execution credential. Machine Key remains in-memory-only until a later delegated human Run authorization contract is designed.
- OIDC callback now lands on `/workspaces` rather than directly on Run Explorer.

## Authorization boundary

The browser route does not grant Workspace access. The server reads the current `workspace_memberships` row for the authenticated session user; disabled users, disabled Workspace/membership records and unsupported actions fail closed. Connection management will consume this server-side authorization in the next slice.

## Current limitations

Workspace creation, invitations and membership editing are not public APIs yet. Existing Workspace/OIDC membership is provisioned by the operator command. Run Explorer continues to require a scoped Machine API Key because the existing Execution APIs authorize machine principals; replacing that safely requires a deliberate delegated-human execution model rather than reusing cookies as an implicit bypass.
