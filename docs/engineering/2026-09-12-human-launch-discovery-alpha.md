# Human Console launch discovery Alpha · 2026-09-12

Mender now exposes a read-only, server-filtered launch projection for authenticated Console humans. This is discovery only: seeing an option does **not** grant `run:create`, reserve budget, or bypass Admission.

## Contract

- Feature gate: `MENDER_CONSOLE_LAUNCH_DISCOVERY_ENABLED=true`; it requires Console OIDC and the existing restricted runtime reader.
- Endpoint: `GET /api/console/v1/workspaces/{workspace_id}/launch-options`.
- Authentication is the HttpOnly Console browser session. The service re-authorizes the current human Membership for `workspace:read` before querying.
- PostgreSQL executes under the existing restricted runtime role and sets the Workspace RLS context before reading the projection.
- The query only returns bindings where Toolset and ToolVersion are published, the binding has an explicit Connection, the Connection and current-user grant are active/not expired, the PriceVersion is active, and an active budget period exists for the same currency.

The browser receives only launch-safe facts: Toolset version, Tool identity/version, title/description/input Schema, side-effect/idempotency annotations, Connection ID, Provider ID, currency and the fixed reserve amount. It does not receive `credential_version_ref`, Provider secrets, Budget/Period IDs, budget balances or remaining quota.

`reserve_micro` is a quoted lower-bound/fixed reservation fact for the currently published PriceVersion, serialized as a decimal string. It is not a promise that the budget still has capacity: final pricing, current Connection grant, budget window, quota availability and arguments Schema are all re-resolved by Admission when a Run is actually started.

## Architecture

The cross-context read model lives in `internal/processes/consolelaunch`, not inside Catalog, Distribution or Connections. Its application layer owns `Authorizer` and `Repository` ports; the PostgreSQL adapter performs the read projection and the Identity ACL maps the published human identity contract. This is an explicit read-side integration and does not give one bounded context another context's write repository.

The frontend API client uses same-origin browser cookies, `cache: no-store`, and validates the returned projection. It fails closed if a successful response contains credential/budget internals or an invalid input Schema.

## Evidence and limits

Focused Go/HTTP tests cover authentication, safe response shape and service invariants. The real isolated PostgreSQL suite verifies RLS plus current Membership/grant filtering with a published Toolset/ToolVersion/Price/Budget fixture.

This slice does not provide Human `run:create`; the next slice introduces a separate short-lived start delegation bound to the exact launch option and a caller-approved charge cap. OIDC Cookie alone remains insufficient to start a Run.
