# Human StartRun delegation Alpha · 2026-09-12

Mender now has an explicit Human → `run:create` capability that is intentionally separate from both the OIDC Browser Session and Machine API Keys. It is designed for one Console launch intent, not as a general-purpose human API credential.

## Exact capability

When `MENDER_CONSOLE_HUMAN_START_ENABLED=true`, an authenticated Console human may mint a short-lived opaque StartRun delegation only after current Workspace Membership authorizes `run:create`. The delegation is bound to:

- Workspace ID;
- Toolset version;
- Tool ID + requested version + immutable ToolVersion ID;
- Connection ID;
- currency;
- maximum charge in micro-units;
- one exact Admission `Idempotency-Key`.

The raw token is returned once. PostgreSQL persists only its SHA-256 digest and the non-secret capability constraints in `identity.run_start_delegations`. Default TTL is 5 minutes and the configured range is 1–10 minutes. Owner/Admin/Developer may mint/use it; Viewer cannot. Every authentication/authorization re-reads current User, Workspace and Membership state, so a downgrade or disable takes effect immediately.

Binding the capability to the `Idempotency-Key` is deliberate: the browser can retry an uncertain HTTP response using the same token/key and converge on Admission replay, but cannot reuse the token with a second idempotency key to create another independent Run.

## Admission reuse

The Console endpoint is `POST /api/console/v1/workspaces/{workspace_id}/runs`. It uses the same strict StartRun JSON contract as the Machine route, but a different Authenticator/Authorizer. The Browser Session cookie itself is never accepted at this endpoint.

The delegated ACL revalidates the exact Toolset/Tool/version/Connection/currency/idempotency key and requires the requested charge cap to be no larger than the delegated cap. The shared Admission Resolver additionally compares the resolved immutable ToolVersion ID against the delegation. From that point onward the existing Admission path is unchanged: canonical arguments, JSON Schema, current Connection grant, PriceVersion, Budget period, quota reservation, Run, Job and Outbox are resolved/created through the same atomic Unit of Work.

This means a stale launch-discovery screen is not authority. If the Toolset is unpublished, Connection revoked, price/budget window changed, input invalid or available quota is insufficient, actual StartRun fails closed even if a delegation was minted earlier.

## Persistence and roles

The browser-session role gains only the Identity table privileges needed to create/read/revoke StartRun delegation facts. It still cannot read Execution, Commerce, Connections or Supply schemas. Admission writes continue through the existing distinct `mender_admission`-style role; the ordinary runtime reader remains the source for immutable plan facts and RLS.

## Evidence and limits

Focused Identity HTTP tests prove exact binding, cap monotonicity, idempotency binding, token digest storage semantics and revocation. Admission ACL tests prove the delegated token cannot widen the launch selection or create a second logical Run.

The isolated PostgreSQL suite mints a real StartRun delegation, persists only its digest, authenticates it through the Identity facade, feeds it into the same `BuildAdmission` service used by Machine StartRun, reserves 75 micro-units for the exact published ToolVersion, and verifies the durable Run admission subject/credential facts. It also proves a role downgrade to Viewer removes StartRun authority.

This slice does not add human billing administration, arbitrary Toolset mutation, long-lived human API keys or direct Browser Session execution. The next Console slice consumes launch discovery + this delegation to provide the minimal select-input-start workflow.
