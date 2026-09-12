# Console Human StartRun workflow Alpha · 2026-09-12

The Mender Console now exposes the first human product loop from server-filtered launch discovery to durable Run admission. The page is a client of existing domain rules; it does not calculate authorization, pricing or quota locally.

## User flow

`/launch` uses the current OIDC Browser Session to load Workspace memberships and launch options. Owner/Admin/Developer may select an option; Viewer can inspect the Workspace but does not receive a Start button.

Each launch option identifies a published Toolset binding, immutable ToolVersion and current-user Connection. The page shows title/description, side-effect/idempotency annotations, Provider/Connection, input JSON Schema and a fixed reserve quote. Arguments are currently entered as a JSON object; schema-driven forms are intentionally deferred.

On **授权并启动** the infrastructure layer:

1. parses arguments as a JSON object;
2. generates a cryptographically random `Idempotency-Key` in the browser;
3. sends Browser Session + CSRF to mint an exact short-lived StartRun delegation;
4. receives the one-time opaque token into current React page memory;
5. sends the existing StartRun body to `/api/console/v1/.../runs` with the token as Bearer and the exact bound `Idempotency-Key`;
6. on confirmed success, best-effort revokes the delegation and clears it from page state;
7. links the resulting Run to Run Explorer.

The token is not placed in URL, React Query keys, localStorage, sessionStorage or cookies. Browser Session cookies are omitted from the actual delegated StartRun request. OIDC authentication and execution capability therefore remain separate.

## Uncertain response recovery

If delegation mint succeeds but StartRun returns an error/timeout before the browser knows whether Admission committed, the page retains `PreparedRunStart` only in current component memory. The operator may retry with the **same delegation token and same Idempotency-Key**. Admission then returns the original Run for a matching request or an idempotency conflict if the payload changed.

The page disables target/arguments/cap editing while a prepared capability exists. “撤销并重置” invalidates the capability before returning to an editable state. This avoids turning a retry token into an authority for another logical Run.

## Fee and schema semantics

The displayed reserve quote is not a budget balance and is not final authorization. `max_charge_micro` is caller consent/cap; lowering it below the server-required reservation makes Admission reject the request, while raising it does not change the published PriceVersion. Admission re-resolves PriceVersion, Budget period and available quota immediately before the atomic reserve/Run/Job/Outbox commit.

The frontend only requires arguments to be a JSON object. It displays the published input Schema for guidance, but does not claim local schema validation as authoritative. The shared Admission Resolver validates canonical arguments using the immutable ToolVersion Schema.

## Architecture

The frontend module follows `domain / application / infrastructure / presentation`:

- `domain` owns only launch projections and local role affordances;
- `application` owns the `LaunchGateway` and short-lived prepared workflow type;
- `infrastructure` is the only layer importing `@mender/api-client`, reading the CSRF cookie or generating the idempotency key;
- `presentation` manages transient user input and prepared token memory;
- `app/router.tsx` remains the Composition Root.

Machine `/api/v1` StartRun remains separate for Agent/service identities. Human `/api/console/v1` uses a different authenticator but the same Admission service contract and Unit of Work.

## Current limits

- no schema-generated forms, file arguments or Artifact input upload;
- no user-editable Toolsets, Catalog publishing or Connection grants;
- no human billing administration or budget modification;
- no durable browser retry queue: refreshing a page deliberately discards the short-lived token, after which Run Explorer can be used to inspect known Runs;
- this remains quota settlement, not a complete payment/revenue ledger.
