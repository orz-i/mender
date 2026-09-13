import { ConsolePolicyDeniedError, createConsoleCatalogClient, createConsoleIdentityClient, MenderApiError } from '@mender/api-client';
import { CatalogLoginRequiredError, CatalogPolicyDeniedError, type CatalogGateway } from '../application/catalog-gateway';

function readCSRFCookie() {
  const prefix = 'mender_csrf=';
  for (const part of document.cookie.split(';')) {
    const value = part.trim();
    if (value.startsWith(prefix)) return decodeURIComponent(value.slice(prefix.length));
  }
  return '';
}

function mapError(error: unknown): never {
  if (error instanceof MenderApiError && error.status === 401) throw new CatalogLoginRequiredError();
  if (error instanceof ConsolePolicyDeniedError) throw new CatalogPolicyDeniedError(error.message, error.policyDecision);
  throw error;
}

function csrf() {
  const value = readCSRFCookie();
  if (!value) throw new Error('CSRF token unavailable');
  return value;
}

export function createCatalogGateway(): CatalogGateway {
  const identity = createConsoleIdentityClient();
  const catalog = createConsoleCatalogClient();
  return {
    async workspaces(signal) {
      try { return (await identity.listWorkspaces(signal)).map((item) => ({ id: item.workspaceId, role: item.role })); }
      catch (error) { return mapError(error); }
    },
    async snapshot(workspaceId, signal) { try { return await catalog.snapshot(workspaceId, signal); } catch (error) { return mapError(error); } },
    async createToolVersion(workspaceId, input, signal) { try { return await catalog.createToolVersion(workspaceId, input, csrf(), signal); } catch (error) { return mapError(error); } },
    async updateToolVersion(workspaceId, input, signal) { try { return await catalog.updateToolVersion(workspaceId, input, csrf(), signal); } catch (error) { return mapError(error); } },
    async toolVersionPreflight(workspaceId, id, signal) { try { return await catalog.toolVersionPreflight(workspaceId, id, csrf(), signal); } catch (error) { return mapError(error); } },
    async requestToolVersionReview(workspaceId, id, signal) { try { return await catalog.requestToolVersionReview(workspaceId, id, csrf(), signal); } catch (error) { return mapError(error); } },
    async publishToolVersion(workspaceId, id, signal) { try { return await catalog.publishToolVersion(workspaceId, id, csrf(), signal); } catch (error) { return mapError(error); } },
    async retireToolVersion(workspaceId, id, signal) { try { return await catalog.retireToolVersion(workspaceId, id, csrf(), signal); } catch (error) { return mapError(error); } },
    async createToolset(workspaceId, id, signal) { try { return await catalog.createToolset(workspaceId, id, csrf(), signal); } catch (error) { return mapError(error); } },
    async upsertBinding(workspaceId, toolsetId, toolVersionId, input, signal) { try { return await catalog.upsertBinding(workspaceId, toolsetId, toolVersionId, input, csrf(), signal); } catch (error) { return mapError(error); } },
    async deleteBinding(workspaceId, toolsetId, toolVersionId, signal) { try { await catalog.deleteBinding(workspaceId, toolsetId, toolVersionId, csrf(), signal); } catch (error) { return mapError(error); } },
    async toolsetPreflight(workspaceId, id, signal) { try { return await catalog.toolsetPreflight(workspaceId, id, csrf(), signal); } catch (error) { return mapError(error); } },
    async requestToolsetReview(workspaceId, id, signal) { try { return await catalog.requestToolsetReview(workspaceId, id, csrf(), signal); } catch (error) { return mapError(error); } },
    async publishToolset(workspaceId, id, signal) { try { return await catalog.publishToolset(workspaceId, id, csrf(), signal); } catch (error) { return mapError(error); } },
    async retireToolset(workspaceId, id, signal) { try { return await catalog.retireToolset(workspaceId, id, csrf(), signal); } catch (error) { return mapError(error); } },
  };
}
