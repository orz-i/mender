import { MenderApiError } from './runs.ts';

export type ConsoleCatalogState = 'draft' | 'published' | 'retired';
export type ConsolePublicationApprovalState = 'pending' | 'approved' | 'rejected' | 'consumed' | 'expired';
export type ConsoleOpenAPIDiagnosticSeverity = 'error' | 'warning';

export interface ConsoleCatalogToolVersion {
  toolVersionId: string;
  toolId: string;
  version: string;
  providerId: string;
  priceVersionId: string;
  deploymentRevision: string;
  title: string;
  description: string;
  inputSchema: Record<string, unknown>;
  outputSchema: Record<string, unknown>;
  sideEffect: 'read_only' | 'write';
  idempotency: 'safe_read' | 'idempotent' | 'unsafe';
  mcpPublishable: boolean;
  revision: string;
  state: ConsoleCatalogState;
  createdAt: string;
  updatedAt: string;
  publishedAt: string | null;
  retiredAt: string | null;
}

function openAPIDiagnostic(value: unknown): ConsoleOpenAPIDiagnostic {
  const raw = object(value);
  exactKeys(raw, ['code', 'severity', 'operation_id', 'path', 'method', 'message'], 'OpenAPI diagnostic');
  const severity = string(raw.severity);
  if (severity !== 'error' && severity !== 'warning') throw new Error('服务返回了无法识别的 OpenAPI diagnostic severity');
  return {
    code: string(raw.code), severity, operationId: optionalString(raw.operation_id), path: optionalString(raw.path), method: optionalString(raw.method), message: string(raw.message),
  };
}

function nullableSchema(value: unknown) {
  if (value === null) return null;
  return schema(value);
}

function openAPIOperation(value: unknown): ConsoleOpenAPIOperation {
  const raw = object(value);
  exactKeys(raw, ['operation_id', 'method', 'path', 'server_url', 'title', 'description', 'input_schema', 'output_schema', 'side_effect', 'idempotency', 'importable', 'diagnostics'], 'OpenAPI operation');
  if (!Array.isArray(raw.diagnostics)) throw new Error('服务返回了无法识别的 OpenAPI diagnostics');
  const sideEffect = string(raw.side_effect); const idempotency = string(raw.idempotency); const importable = boolean(raw.importable);
  if (sideEffect !== 'read_only' && sideEffect !== 'write') throw new Error('服务返回了无法识别的 OpenAPI side effect');
  if (!['safe_read', 'idempotent', 'unsafe'].includes(idempotency)) throw new Error('服务返回了无法识别的 OpenAPI idempotency');
  const operation: ConsoleOpenAPIOperation = {
    operationId: string(raw.operation_id), method: string(raw.method), path: string(raw.path), serverUrl: typeof raw.server_url === 'string' ? raw.server_url : '',
    title: nullableOrEmptyString(raw.title), description: nullableOrEmptyString(raw.description), inputSchema: nullableSchema(raw.input_schema), outputSchema: nullableSchema(raw.output_schema),
    sideEffect, idempotency: idempotency as ConsoleOpenAPIOperation['idempotency'], importable, diagnostics: raw.diagnostics.map(openAPIDiagnostic),
  };
  if (operation.importable && (operation.method !== 'POST' || !operation.serverUrl.startsWith('https://') || operation.inputSchema === null || operation.outputSchema === null || operation.sideEffect !== 'write' || operation.idempotency !== 'unsafe' || operation.diagnostics.some((item) => item.severity === 'error'))) {
    throw new Error('服务返回了不一致的 OpenAPI importable contract');
  }
  return operation;
}

function openAPIPreview(value: unknown): ConsoleOpenAPIPreview {
  const raw = object(value);
  exactKeys(raw, ['openapi_version', 'title', 'operations', 'diagnostics'], 'OpenAPI preview');
  if (!Array.isArray(raw.operations) || !Array.isArray(raw.diagnostics)) throw new Error('服务返回了无法识别的 OpenAPI preview');
  return { openapiVersion: string(raw.openapi_version), title: nullableOrEmptyString(raw.title), operations: raw.operations.map(openAPIOperation), diagnostics: raw.diagnostics.map(openAPIDiagnostic) };
}

function openAPIImportResult(value: unknown): ConsoleOpenAPIImportResult {
  const raw = object(value);
  exactKeys(raw, ['tool_version', 'source_operation'], 'OpenAPI import result');
  const importedToolRaw = object(raw.tool_version);
  exactKeys(importedToolRaw, ['tool_version_id', 'tool_id', 'version', 'provider_id', 'price_version_id', 'deployment_revision', 'title', 'description', 'input_schema', 'output_schema', 'side_effect', 'idempotency', 'mcp_publishable', 'revision', 'state', 'created_at', 'updated_at', 'published_at', 'retired_at'], 'OpenAPI imported ToolVersion');
  const toolVersion = tool(importedToolRaw); const sourceOperation = openAPIOperation(raw.source_operation);
  if (!sourceOperation.importable || toolVersion.state !== 'draft' || toolVersion.mcpPublishable || toolVersion.sideEffect !== sourceOperation.sideEffect || toolVersion.idempotency !== sourceOperation.idempotency) throw new Error('服务返回了不一致的 OpenAPI import result');
  return { toolVersion, sourceOperation };
}

function nullableOrEmptyString(value: unknown) {
  if (typeof value !== 'string') throw new Error('服务返回了无法识别的 Catalog 响应');
  return value;
}

function exactKeys(raw: Record<string, unknown>, expected: string[], label: string) {
  const actual = Object.keys(raw).sort(); const wanted = [...expected].sort();
  if (actual.length !== wanted.length || actual.some((key, index) => key !== wanted[index])) throw new Error(`服务返回了无法识别的 ${label}`);
}

export interface ConsoleOpenAPIDiagnostic {
  code: string;
  severity: ConsoleOpenAPIDiagnosticSeverity;
  operationId: string | null;
  path: string | null;
  method: string | null;
  message: string;
}

export interface ConsoleOpenAPIOperation {
  operationId: string;
  method: string;
  path: string;
  serverUrl: string;
  title: string;
  description: string;
  inputSchema: Record<string, unknown> | null;
  outputSchema: Record<string, unknown> | null;
  sideEffect: 'read_only' | 'write';
  idempotency: 'safe_read' | 'idempotent' | 'unsafe';
  importable: boolean;
  diagnostics: ConsoleOpenAPIDiagnostic[];
}

export interface ConsoleOpenAPIPreview {
  openapiVersion: string;
  title: string;
  operations: ConsoleOpenAPIOperation[];
  diagnostics: ConsoleOpenAPIDiagnostic[];
}

export interface ConsoleOpenAPIImportInput {
  document: string;
  operationId: string;
  toolVersionId: string;
  toolId: string;
  version: string;
  providerId: string;
  priceVersionId: string;
  deploymentRevision: string;
}

export interface ConsoleOpenAPIImportResult {
  toolVersion: ConsoleCatalogToolVersion;
  sourceOperation: ConsoleOpenAPIOperation;
}

function policyDecision(value: unknown): ConsolePublicationPolicyDecision {
  const raw = object(value);
  for (const forbidden of ['credential_version_ref', 'secret', 'token', 'arguments', 'artifact_body', 'reserve_micro', 'limit_micro', 'charge_micro', 'payment', 'invoice', 'revenue']) if (forbidden in raw) throw new Error('服务返回了不应暴露给 publication policy 的敏感字段');
  const kind = string(raw.target_kind); if (kind !== 'tool_version' && kind !== 'toolset') throw new Error('服务返回了无法识别的 publication policy target');
  const risk = string(raw.risk_level); if (!['low', 'medium', 'high', 'critical'].includes(risk)) throw new Error('服务返回了无法识别的 publication risk');
  const outcome = string(raw.outcome); if (outcome !== 'allow' && outcome !== 'deny') throw new Error('服务返回了无法识别的 publication policy outcome');
  if (!Array.isArray(raw.reason_codes) || raw.reason_codes.length === 0 || raw.reason_codes.some((item) => typeof item !== 'string' || item.length === 0)) throw new Error('服务返回了无法识别的 publication policy reasons');
  return { sequence: exactIntegerString(raw.sequence), policyRevisionId: string(raw.policy_revision_id), policyRevision: exactIntegerString(raw.policy_revision), targetKind: kind, targetId: string(raw.target_id), targetRevision: exactIntegerString(raw.target_revision), riskLevel: risk as ConsolePublicationPolicyDecision['riskLevel'], outcome, reasonCodes: raw.reason_codes as string[], evaluatedAt: string(raw.evaluated_at) };
}

function submission(value: unknown): ConsolePublicationSubmission {
  const raw = object(value);
  return { approval: raw.approval === undefined || raw.approval === null ? null : approval(raw.approval), policyDecision: policyDecision(raw.policy_decision) };
}

function approval(value: unknown): ConsolePublicationApproval {
  const raw = object(value);
  for (const forbidden of ['credential_version_ref', 'secret', 'token', 'reserve_micro', 'limit_micro', 'consumed_micro', 'charged_micro']) {
    if (forbidden in raw) throw new Error('服务返回了不应暴露给 publication approval 的敏感字段');
  }
  const targetKind = string(raw.target_kind);
  if (targetKind !== 'tool_version' && targetKind !== 'toolset') throw new Error('服务返回了无法识别的 publication approval target');
  return {
    id: string(raw.id), targetKind, targetId: string(raw.target_id), targetRevision: exactIntegerString(raw.target_revision),
    requesterUserId: string(raw.requester_user_id), state: approvalState(raw.state), requestedAt: string(raw.requested_at), expiresAt: string(raw.expires_at),
    reviewerUserId: optionalString(raw.reviewer_user_id), reviewedAt: optionalString(raw.reviewed_at), decisionNote: typeof raw.decision_note === 'string' ? raw.decision_note : '', consumedAt: optionalString(raw.consumed_at),
  };
}

function approvalState(value: unknown): ConsolePublicationApprovalState {
  const raw = string(value);
  if (!['pending', 'approved', 'rejected', 'consumed', 'expired'].includes(raw)) throw new Error('服务返回了无法识别的 publication approval 状态');
  return raw as ConsolePublicationApprovalState;
}

export interface ConsolePublicationApproval {
  id: string;
  targetKind: 'tool_version' | 'toolset';
  targetId: string;
  targetRevision: string;
  requesterUserId: string;
  state: ConsolePublicationApprovalState;
  requestedAt: string;
  expiresAt: string;
  reviewerUserId: string | null;
  reviewedAt: string | null;
  decisionNote: string;
  consumedAt: string | null;
}

export interface ConsolePublicationPolicyDecision {
  sequence: string;
  policyRevisionId: string;
  policyRevision: string;
  targetKind: 'tool_version' | 'toolset';
  targetId: string;
  targetRevision: string;
  riskLevel: 'low' | 'medium' | 'high' | 'critical';
  outcome: 'allow' | 'deny';
  reasonCodes: string[];
  evaluatedAt: string;
}

export interface ConsolePublicationSubmission {
  approval: ConsolePublicationApproval | null;
  policyDecision: ConsolePublicationPolicyDecision;
}

export class ConsolePolicyDeniedError extends MenderApiError {
  readonly policyDecision: ConsolePublicationPolicyDecision;
  constructor(status: number, message: string, policyDecision: ConsolePublicationPolicyDecision) {
    super(status, 'POLICY_DENIED', message);
    this.policyDecision = policyDecision;
  }
}

export interface ConsoleCatalogBinding {
  toolId: string;
  toolVersionLabel: string;
  toolVersionId: string;
  budgetId: string;
  connectionId: string;
  mcpName: string;
  mcpExposed: boolean;
  state: ConsoleCatalogState;
  publishedAt: string | null;
}

export interface ConsoleCatalogToolset {
  id: string;
  revision: string;
  state: ConsoleCatalogState;
  createdAt: string;
  updatedAt: string;
  publishedAt: string | null;
  retiredAt: string | null;
  bindings: ConsoleCatalogBinding[];
}

export interface ConsoleCatalogConnectionOption {
  connectionId: string;
  providerId: string;
  state: string;
  revision: string;
  createdAt: string;
  expiresAt: string;
}

export interface ConsoleCatalogPriceOption {
  id: string;
  toolVersionId: string;
  currency: string;
  reserveMicro: string;
  startsAt: string;
  endsAt: string;
  active: boolean;
}

export interface ConsoleCatalogBudgetOption {
  budgetId: string;
  periodId: string;
  currency: string;
  startsAt: string;
  endsAt: string;
  active: boolean;
}

export interface ConsoleCatalogSnapshot {
  toolVersions: ConsoleCatalogToolVersion[];
  toolsets: ConsoleCatalogToolset[];
  connections: ConsoleCatalogConnectionOption[];
  priceVersions: ConsoleCatalogPriceOption[];
  budgetPeriods: ConsoleCatalogBudgetOption[];
  publicationApprovals: ConsolePublicationApproval[];
}

export interface ConsoleCatalogIssue { code: string; targetId: string }
export interface ConsoleCatalogPreflight { ready: boolean; issues: ConsoleCatalogIssue[] }

export interface ConsoleCatalogToolVersionInput {
  toolVersionId: string;
  toolId: string;
  version: string;
  providerId: string;
  priceVersionId: string;
  deploymentRevision: string;
  title: string;
  description: string;
  inputSchema: Record<string, unknown>;
  outputSchema: Record<string, unknown>;
  sideEffect: 'read_only' | 'write';
  idempotency: 'safe_read' | 'idempotent' | 'unsafe';
  mcpPublishable: boolean;
}

export interface ConsoleCatalogBindingInput {
  toolId: string;
  toolVersionLabel: string;
  budgetId: string;
  connectionId: string;
  mcpName: string;
  mcpExposed: boolean;
}

function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('服务返回了无法识别的 Catalog 响应');
  return value as Record<string, unknown>;
}

function string(value: unknown) {
  if (typeof value !== 'string' || value.length === 0) throw new Error('服务返回了无法识别的 Catalog 响应');
  return value;
}

function optionalString(value: unknown) {
  if (value === null) return null;
  return string(value);
}

function boolean(value: unknown) {
  if (typeof value !== 'boolean') throw new Error('服务返回了无法识别的 Catalog 响应');
  return value;
}

function exactIntegerString(value: unknown) {
  const raw = string(value);
  if (!/^[0-9]+$/.test(raw)) throw new Error('服务返回了无法识别的 fixed-precision 值');
  return raw;
}

function state(value: unknown): ConsoleCatalogState {
  const raw = string(value);
  if (raw !== 'draft' && raw !== 'published' && raw !== 'retired') throw new Error('服务返回了无法识别的 Catalog 状态');
  return raw;
}

function schema(value: unknown) {
  return object(value);
}

function tool(value: unknown): ConsoleCatalogToolVersion {
  const raw = object(value);
  const sideEffect = string(raw.side_effect);
  const idempotency = string(raw.idempotency);
  if (sideEffect !== 'read_only' && sideEffect !== 'write') throw new Error('服务返回了无法识别的 ToolVersion contract');
  if (!['safe_read', 'idempotent', 'unsafe'].includes(idempotency)) throw new Error('服务返回了无法识别的 ToolVersion contract');
  return {
    toolVersionId: string(raw.tool_version_id), toolId: string(raw.tool_id), version: string(raw.version),
    providerId: string(raw.provider_id), priceVersionId: string(raw.price_version_id), deploymentRevision: string(raw.deployment_revision),
    title: string(raw.title), description: typeof raw.description === 'string' ? raw.description : '',
    inputSchema: schema(raw.input_schema), outputSchema: schema(raw.output_schema), sideEffect, idempotency: idempotency as ConsoleCatalogToolVersion['idempotency'],
    mcpPublishable: boolean(raw.mcp_publishable), revision: exactIntegerString(raw.revision), state: state(raw.state), createdAt: string(raw.created_at), updatedAt: string(raw.updated_at),
    publishedAt: optionalString(raw.published_at), retiredAt: optionalString(raw.retired_at),
  };
}

function binding(value: unknown): ConsoleCatalogBinding {
  const raw = object(value);
  return {
    toolId: string(raw.tool_id), toolVersionLabel: string(raw.tool_version_label), toolVersionId: string(raw.tool_version_id),
    budgetId: string(raw.budget_id), connectionId: string(raw.connection_id), mcpName: typeof raw.mcp_name === 'string' ? raw.mcp_name : '',
    mcpExposed: boolean(raw.mcp_exposed), state: state(raw.state), publishedAt: optionalString(raw.published_at),
  };
}

function toolset(value: unknown): ConsoleCatalogToolset {
  const raw = object(value);
  if (!Array.isArray(raw.bindings)) throw new Error('服务返回了无法识别的 Toolset 响应');
  return {
    id: string(raw.id), revision: exactIntegerString(raw.revision), state: state(raw.state), createdAt: string(raw.created_at), updatedAt: string(raw.updated_at),
    publishedAt: optionalString(raw.published_at), retiredAt: optionalString(raw.retired_at), bindings: raw.bindings.map(binding),
  };
}

function connection(value: unknown): ConsoleCatalogConnectionOption {
  const raw = object(value);
  for (const forbidden of ['credential_version_ref', 'secret', 'token']) if (forbidden in raw) throw new Error('服务返回了不允许暴露的 Connection 字段');
  return {
    connectionId: string(raw.connection_id), providerId: string(raw.provider_id), state: string(raw.state), revision: exactIntegerString(raw.revision),
    createdAt: string(raw.created_at), expiresAt: string(raw.expires_at),
  };
}

function price(value: unknown): ConsoleCatalogPriceOption {
  const raw = object(value);
  for (const forbidden of ['charge_micro', 'billing_policy']) if (forbidden in raw) throw new Error('服务返回了不应暴露给 Catalog 的 settlement 字段');
  return {
    id: string(raw.id), toolVersionId: string(raw.tool_version_id), currency: string(raw.currency), reserveMicro: exactIntegerString(raw.reserve_micro),
    startsAt: string(raw.starts_at), endsAt: string(raw.ends_at), active: boolean(raw.active),
  };
}

function budget(value: unknown): ConsoleCatalogBudgetOption {
  const raw = object(value);
  for (const forbidden of ['limit_micro', 'consumed_micro', 'reserved_micro', 'available_micro']) if (forbidden in raw) throw new Error('服务返回了不应暴露给 Catalog 的 Budget amount 字段');
  return {
    budgetId: string(raw.budget_id), periodId: string(raw.period_id), currency: string(raw.currency), startsAt: string(raw.starts_at), endsAt: string(raw.ends_at), active: boolean(raw.active),
  };
}

function preflight(value: unknown): ConsoleCatalogPreflight {
  const raw = object(value);
  if (!Array.isArray(raw.issues)) throw new Error('服务返回了无法识别的 preflight 响应');
  return { ready: boolean(raw.ready), issues: raw.issues.map((item) => { const issue = object(item); return { code: string(issue.code), targetId: string(issue.target_id) }; }) };
}

async function failure(response: Response) {
  try {
    const raw = object(await response.json()); const error = object(raw.error); const code = string(error.code); const message = string(error.message);
    if (code === 'POLICY_DENIED') {
      const data = object(raw.data);
      return new ConsolePolicyDeniedError(response.status, message, policyDecision(data.policy_decision));
    }
    return new MenderApiError(response.status, code, message);
  } catch (error) {
    if (error instanceof MenderApiError) return error;
    return new MenderApiError(response.status, 'HTTP_ERROR', `Mender API 请求失败（HTTP ${response.status}）`);
  }
}

async function request(fetcher: typeof fetch, url: string, init: RequestInit = {}) {
  const timeout = AbortSignal.timeout(5_000); const signal = init.signal ? AbortSignal.any([init.signal, timeout]) : timeout;
  const response = await fetcher(url, { ...init, signal, cache: 'no-store', credentials: 'same-origin', referrerPolicy: 'same-origin', headers: { Accept: 'application/json', ...init.headers } });
  if (!response.ok) throw await failure(response);
  return response;
}

function csrf(value: string) {
  if (!value || value.length > 256) throw new Error('Catalog mutation requires a valid CSRF token');
  return value;
}

function toolBody(input: ConsoleCatalogToolVersionInput) {
  return JSON.stringify({
    tool_version_id: input.toolVersionId, tool_id: input.toolId, version: input.version, provider_id: input.providerId,
    price_version_id: input.priceVersionId, deployment_revision: input.deploymentRevision, title: input.title, description: input.description,
    input_schema: input.inputSchema, output_schema: input.outputSchema, side_effect: input.sideEffect, idempotency: input.idempotency, mcp_publishable: input.mcpPublishable,
  });
}

export function createConsoleCatalogClient(baseUrl = '', fetcher: typeof fetch = fetch) {
  const base = baseUrl.replace(/\/$/, '');
  const root = (workspaceId: string) => `${base}/api/console/v1/workspaces/${encodeURIComponent(workspaceId)}/catalog`;
  const mutation = (csrfToken: string): HeadersInit => ({ 'X-Mender-CSRF': csrf(csrfToken) });
  return {
    async snapshot(workspaceId: string, signal?: AbortSignal): Promise<ConsoleCatalogSnapshot> {
      if (!workspaceId) throw new Error('Workspace ID is required');
      const response = await request(fetcher, root(workspaceId), { signal }); const raw = object(await response.json()); const data = object(raw.data);
      if (!Array.isArray(data.tool_versions) || !Array.isArray(data.toolsets) || !Array.isArray(data.connections) || !Array.isArray(data.price_versions) || !Array.isArray(data.budget_periods) || !Array.isArray(data.publication_approvals)) throw new Error('服务返回了无法识别的 Catalog snapshot');
      return { toolVersions: data.tool_versions.map(tool), toolsets: data.toolsets.map(toolset), connections: data.connections.map(connection), priceVersions: data.price_versions.map(price), budgetPeriods: data.budget_periods.map(budget), publicationApprovals: data.publication_approvals.map(approval) };
    },
    async previewOpenAPI(workspaceId: string, document: string, signal?: AbortSignal): Promise<ConsoleOpenAPIPreview> {
      if (!workspaceId) throw new Error('Workspace ID is required');
      if (document.length < 2 || document.length > 1_048_576 || document.includes('\0')) throw new Error('OpenAPI document must be an inline document up to 1 MiB');
      const response = await request(fetcher, `${root(workspaceId)}/openapi/preview`, { method: 'POST', signal, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ document }) });
      return openAPIPreview(object(await response.json()).data);
    },
    async importOpenAPIOperation(workspaceId: string, input: ConsoleOpenAPIImportInput, csrfToken: string, signal?: AbortSignal): Promise<ConsoleOpenAPIImportResult> {
      if (!workspaceId) throw new Error('Workspace ID is required');
      if (input.document.length < 2 || input.document.length > 1_048_576 || input.document.includes('\0')) throw new Error('OpenAPI document must be an inline document up to 1 MiB');
      const response = await request(fetcher, `${root(workspaceId)}/openapi/import`, {
        method: 'POST', signal, headers: { ...mutation(csrfToken), 'Content-Type': 'application/json' },
        body: JSON.stringify({ document: input.document, operation_id: input.operationId, tool_version_id: input.toolVersionId, tool_id: input.toolId, version: input.version, provider_id: input.providerId, price_version_id: input.priceVersionId, deployment_revision: input.deploymentRevision }),
      });
      const result = openAPIImportResult(object(await response.json()).data);
      if (result.sourceOperation.operationId !== input.operationId || result.toolVersion.toolVersionId !== input.toolVersionId || result.toolVersion.toolId !== input.toolId || result.toolVersion.version !== input.version || result.toolVersion.providerId !== input.providerId || result.toolVersion.priceVersionId !== input.priceVersionId || result.toolVersion.deploymentRevision !== input.deploymentRevision) throw new Error('服务返回了与 OpenAPI import 请求不一致的 draft');
      return result;
    },
    async requestToolsetReview(workspaceId: string, toolsetId: string, csrfToken: string, signal?: AbortSignal) {
      const response = await request(fetcher, `${root(workspaceId)}/toolsets/${encodeURIComponent(toolsetId)}/review-requests`, { method: 'POST', signal, headers: mutation(csrfToken) });
      return submission(object(await response.json()).data);
    },
    async requestToolVersionReview(workspaceId: string, toolVersionId: string, csrfToken: string, signal?: AbortSignal) {
      const response = await request(fetcher, `${root(workspaceId)}/tool-versions/${encodeURIComponent(toolVersionId)}/review-requests`, { method: 'POST', signal, headers: mutation(csrfToken) });
      return submission(object(await response.json()).data);
    },
    async createToolVersion(workspaceId: string, input: ConsoleCatalogToolVersionInput, csrfToken: string, signal?: AbortSignal) {
      const response = await request(fetcher, `${root(workspaceId)}/tool-versions`, { method: 'POST', signal, headers: { ...mutation(csrfToken), 'Content-Type': 'application/json' }, body: toolBody(input) });
      return tool(object(await response.json()).data);
    },
    async updateToolVersion(workspaceId: string, input: ConsoleCatalogToolVersionInput, csrfToken: string, signal?: AbortSignal) {
      const response = await request(fetcher, `${root(workspaceId)}/tool-versions/${encodeURIComponent(input.toolVersionId)}`, { method: 'PUT', signal, headers: { ...mutation(csrfToken), 'Content-Type': 'application/json' }, body: toolBody(input) });
      return tool(object(await response.json()).data);
    },
    async toolVersionPreflight(workspaceId: string, toolVersionId: string, csrfToken: string, signal?: AbortSignal) {
      const response = await request(fetcher, `${root(workspaceId)}/tool-versions/${encodeURIComponent(toolVersionId)}/preflight`, { method: 'POST', signal, headers: mutation(csrfToken) });
      return preflight(object(await response.json()).data);
    },
    async publishToolVersion(workspaceId: string, toolVersionId: string, csrfToken: string, signal?: AbortSignal) {
      const response = await request(fetcher, `${root(workspaceId)}/tool-versions/${encodeURIComponent(toolVersionId)}/publish`, { method: 'POST', signal, headers: mutation(csrfToken) });
      return tool(object(await response.json()).data);
    },
    async retireToolVersion(workspaceId: string, toolVersionId: string, csrfToken: string, signal?: AbortSignal) {
      const response = await request(fetcher, `${root(workspaceId)}/tool-versions/${encodeURIComponent(toolVersionId)}/retire`, { method: 'POST', signal, headers: mutation(csrfToken) });
      return tool(object(await response.json()).data);
    },
    async createToolset(workspaceId: string, id: string, csrfToken: string, signal?: AbortSignal) {
      const response = await request(fetcher, `${root(workspaceId)}/toolsets`, { method: 'POST', signal, headers: { ...mutation(csrfToken), 'Content-Type': 'application/json' }, body: JSON.stringify({ id }) });
      return toolset(object(await response.json()).data);
    },
    async upsertBinding(workspaceId: string, toolsetId: string, toolVersionId: string, input: ConsoleCatalogBindingInput, csrfToken: string, signal?: AbortSignal) {
      const response = await request(fetcher, `${root(workspaceId)}/toolsets/${encodeURIComponent(toolsetId)}/bindings/${encodeURIComponent(toolVersionId)}`, {
        method: 'PUT', signal, headers: { ...mutation(csrfToken), 'Content-Type': 'application/json' },
        body: JSON.stringify({ tool_id: input.toolId, tool_version_label: input.toolVersionLabel, budget_id: input.budgetId, connection_id: input.connectionId, mcp_name: input.mcpName, mcp_exposed: input.mcpExposed }),
      });
      return binding(object(await response.json()).data);
    },
    async deleteBinding(workspaceId: string, toolsetId: string, toolVersionId: string, csrfToken: string, signal?: AbortSignal) {
      await request(fetcher, `${root(workspaceId)}/toolsets/${encodeURIComponent(toolsetId)}/bindings/${encodeURIComponent(toolVersionId)}`, { method: 'DELETE', signal, headers: mutation(csrfToken) });
    },
    async toolsetPreflight(workspaceId: string, toolsetId: string, csrfToken: string, signal?: AbortSignal) {
      const response = await request(fetcher, `${root(workspaceId)}/toolsets/${encodeURIComponent(toolsetId)}/preflight`, { method: 'POST', signal, headers: mutation(csrfToken) });
      return preflight(object(await response.json()).data);
    },
    async publishToolset(workspaceId: string, toolsetId: string, csrfToken: string, signal?: AbortSignal) {
      const response = await request(fetcher, `${root(workspaceId)}/toolsets/${encodeURIComponent(toolsetId)}/publish`, { method: 'POST', signal, headers: mutation(csrfToken) });
      return toolset(object(await response.json()).data);
    },
    async retireToolset(workspaceId: string, toolsetId: string, csrfToken: string, signal?: AbortSignal) {
      const response = await request(fetcher, `${root(workspaceId)}/toolsets/${encodeURIComponent(toolsetId)}/retire`, { method: 'POST', signal, headers: mutation(csrfToken) });
      return toolset(object(await response.json()).data);
    },
  };
}
