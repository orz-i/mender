import { createAdminGovernanceClient, createConsoleIdentityClient, MenderApiError } from '@mender/api-client';
import { PolicyLoginRequiredError, type PolicyGateway } from '../application/policy-gateway';

function readCSRFCookie() {
  const prefix = 'mender_csrf=';
  for (const part of document.cookie.split(';')) {
    const value = part.trim();
    if (value.startsWith(prefix)) return decodeURIComponent(value.slice(prefix.length));
  }
  return '';
}
function csrf() { const value = readCSRFCookie(); if (!value) throw new Error('CSRF token unavailable'); return value; }
function mapError(error: unknown): never { if (error instanceof MenderApiError && error.status === 401) throw new PolicyLoginRequiredError(); throw error; }

export function createPolicyGateway(): PolicyGateway {
  const identity = createConsoleIdentityClient();
  const governance = createAdminGovernanceClient();
  return {
    async workspaces(signal) { try { return (await identity.listWorkspaces(signal)).map((item) => ({ id: item.workspaceId, role: item.role })); } catch (error) { return mapError(error); } },
    async snapshot(workspaceId, signal) { try { return await governance.policy(workspaceId, signal); } catch (error) { return mapError(error); } },
    async create(workspaceId, input, signal) { try { return await governance.createPolicy(workspaceId, input, csrf(), signal); } catch (error) { return mapError(error); } },
    async activate(workspaceId, policyId, signal) { try { return await governance.activatePolicy(workspaceId, policyId, csrf(), signal); } catch (error) { return mapError(error); } },
  };
}
