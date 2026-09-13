import { createAdminGovernanceClient, createConsoleIdentityClient, MenderApiError } from '@mender/api-client';
import { ReviewLoginRequiredError, type ReviewGateway } from '../application/review-gateway';

function readCSRFCookie() {
  const prefix = 'mender_csrf=';
  for (const part of document.cookie.split(';')) {
    const value = part.trim();
    if (value.startsWith(prefix)) return decodeURIComponent(value.slice(prefix.length));
  }
  return '';
}
function csrf() { const value = readCSRFCookie(); if (!value) throw new Error('CSRF token unavailable'); return value; }
function mapError(error: unknown): never { if (error instanceof MenderApiError && error.status === 401) throw new ReviewLoginRequiredError(); throw error; }

export function createReviewGateway(): ReviewGateway {
  const identity = createConsoleIdentityClient();
  const governance = createAdminGovernanceClient();
  return {
    async workspaces(signal) { try { return (await identity.listWorkspaces(signal)).map((item) => ({ id: item.workspaceId, role: item.role })); } catch (error) { return mapError(error); } },
    async list(workspaceId, signal) { try { return await governance.list(workspaceId, signal); } catch (error) { return mapError(error); } },
    async approve(workspaceId, approvalId, note, signal) { try { return await governance.approve(workspaceId, approvalId, note, csrf(), signal); } catch (error) { return mapError(error); } },
    async reject(workspaceId, approvalId, note, signal) { try { return await governance.reject(workspaceId, approvalId, note, csrf(), signal); } catch (error) { return mapError(error); } },
  };
}
