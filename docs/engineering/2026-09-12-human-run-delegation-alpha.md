# Human → Run explicit delegation Alpha · 2026-09-12

Mender now has an explicit short-lived Human → Run authorization contract. The OIDC browser session itself is **not** accepted by Run endpoints and does not inherit Machine Key authority.

## Delivered

- Migration `0022_run_delegations.sql` stores only SHA-256 digests for opaque delegation tokens. Raw tokens are returned once and are never written to the database.
- Console can issue/revoke delegations only through an authenticated human session plus `X-Mender-CSRF`.
- Delegations are Workspace-bound, User-bound and limited to `run:read` plus optional `run:cancel`. `run:create` is intentionally absent.
- TTL is reviewed configuration, default 10 minutes and bounded to 1–30 minutes.
- Every delegation authentication/authorization re-reads current User, Workspace and Membership state. A role downgrade from Admin/Developer to Viewer immediately removes delegated cancel authority while retaining read authority.
- A dedicated delegated execution anti-corruption layer implements the existing Execution ports. Machine `/api/v1/...` and delegated `/api/console/v1/...` surfaces have different authenticators even though both use an explicit Bearer token.
- Coordinated/provider cancellation can be composed for delegated requests without reusing Machine Key authorization.
- Browser-session DB role remains identity-only. It can create/revoke delegation facts but cannot access Execution, Commerce, Connections or Supply data. Execution reads still run through the existing restricted Run API database role and RLS.

## Console endpoints

```text
POST   /api/console/v1/workspaces/{workspace_id}/run-delegations
DELETE /api/console/v1/workspaces/{workspace_id}/run-delegations/{delegation_id}

GET    /api/console/v1/workspaces/{workspace_id}/runs
GET    /api/console/v1/workspaces/{workspace_id}/runs/{run_id}
GET    /api/console/v1/workspaces/{workspace_id}/runs/{run_id}/events
GET    /api/console/v1/workspaces/{workspace_id}/runs/{run_id}/artifacts
POST   /api/console/v1/workspaces/{workspace_id}/runs/{run_id}/cancel
```

The first two use the browser session + CSRF. The Run endpoints require the newly issued delegation Bearer token. The browser Cookie alone is insufficient.

## Verification

- Focused unit/HTTP tests verify strict scopes, CSRF issuance, one-time raw token response, `run:create` denial, revocation and fail-closed authentication.
- Real isolated PostgreSQL verifies digest-only persistence, current membership revalidation, Admin→Viewer cancel revocation, read continuity and explicit delegation revocation.
- Existing Machine Key Run endpoints remain separate and unchanged.

## Not delivered

This slice does not add human `run:create`, billing authority, delegated Toolset mutation, public user registration or indefinite browser execution credentials. Console adoption of the delegation flow is the next slice; until then the existing Run Explorer UI may still show its Alpha Machine Key form.
