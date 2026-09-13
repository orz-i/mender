import type { CatalogBinding, CatalogBindingInput, CatalogPreflight, CatalogSnapshot, CatalogToolVersion, CatalogToolVersionInput, CatalogToolset, CatalogWorkspace } from '../domain/catalog';

export class CatalogLoginRequiredError extends Error {}

export interface CatalogGateway {
  workspaces(signal?: AbortSignal): Promise<CatalogWorkspace[]>;
  snapshot(workspaceId: string, signal?: AbortSignal): Promise<CatalogSnapshot>;
  createToolVersion(workspaceId: string, input: CatalogToolVersionInput, signal?: AbortSignal): Promise<CatalogToolVersion>;
  updateToolVersion(workspaceId: string, input: CatalogToolVersionInput, signal?: AbortSignal): Promise<CatalogToolVersion>;
  toolVersionPreflight(workspaceId: string, toolVersionId: string, signal?: AbortSignal): Promise<CatalogPreflight>;
  publishToolVersion(workspaceId: string, toolVersionId: string, signal?: AbortSignal): Promise<CatalogToolVersion>;
  retireToolVersion(workspaceId: string, toolVersionId: string, signal?: AbortSignal): Promise<CatalogToolVersion>;
  createToolset(workspaceId: string, id: string, signal?: AbortSignal): Promise<CatalogToolset>;
  upsertBinding(workspaceId: string, toolsetId: string, toolVersionId: string, input: CatalogBindingInput, signal?: AbortSignal): Promise<CatalogBinding>;
  deleteBinding(workspaceId: string, toolsetId: string, toolVersionId: string, signal?: AbortSignal): Promise<void>;
  toolsetPreflight(workspaceId: string, toolsetId: string, signal?: AbortSignal): Promise<CatalogPreflight>;
  publishToolset(workspaceId: string, toolsetId: string, signal?: AbortSignal): Promise<CatalogToolset>;
  retireToolset(workspaceId: string, toolsetId: string, signal?: AbortSignal): Promise<CatalogToolset>;
}
