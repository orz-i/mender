# Connection self-service Alpha · 2026-09-12

Mender Console now exposes the first human Connection management slice on top of the OIDC browser session and Workspace membership contract.

## Delivered

- `MENDER_CONSOLE_CONNECTIONS_ENABLED=true` explicitly registers `/api/console/v1/workspaces/{workspace_id}/connections` only when Console OIDC is enabled.
- A separate connection-manager principal is provisioned with `pnpm db:grant-connection-manager --role ...`. It can read only `workspace_id`, Connection ID, Provider ID, state, revision and timestamps, and can update only state/revision for revoke.
- The Connection manager cannot read `credential_version_ref`, Connection grants, human/machine identity, Execution, Commerce or Supply schemas. RLS still requires the per-transaction Workspace setting.
- GET lists at most 200 safe Connection summaries for the server-authorized Workspace. DELETE is idempotent and changes a Connection to `revoked`; only current Owner/Admin memberships have `connection:manage`.
- Browser revoke requires the HttpOnly human session plus `X-Mender-CSRF`. The readable CSRF cookie is host-only/SameSite and scoped to `/` so Console pages can echo it; it is not an authentication credential.
- `@mender/api-client` rejects a successful payload if it unexpectedly contains `credential_version_ref`, `secret` or `token` fields.
- Console `/connections` lists safe metadata and exposes revoke only to Owner/Admin as a UX affordance; the server performs the authoritative membership check again.

## Not delivered

This slice intentionally does not create or upload BYOK secrets, start OAuth authorization, rotate credentials, edit Connection grants or expose supplier account contents. Creation requires a separately reviewed secret-ingestion/OAuth flow so the browser never becomes a durable secret store. Existing operator/database fixtures remain the way to seed Connection credentials during Alpha.

Machine Run APIs remain unchanged for Agent/service identities. Console now has a separate explicit short-lived Human→Run delegation for read/cancel; Connection access still does not imply `run:create` or billing authority, and the OIDC Cookie itself is never accepted as a Run credential.
