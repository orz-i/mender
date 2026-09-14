import { createAdminGovernanceClient, createConsoleIdentityClient, MenderApiError } from '@mender/api-client';
import { ExecutionGovernanceLoginRequiredError, type ExecutionGovernanceGateway } from '../application/execution-governance-gateway';
import type { ExecutionConfirmation, ExecutionGovernanceSnapshot, ExecutionPolicyDecision, ExecutionPolicyRevision } from '../domain/execution-governance';

function readCSRFCookie() {
  const prefix = 'mender_csrf=';
  for (const part of document.cookie.split(';')) {
    const value = part.trim();
    if (value.startsWith(prefix)) return decodeURIComponent(value.slice(prefix.length));
  }
  return '';
}
function csrf() { const value = readCSRFCookie(); if (!value) throw new Error('CSRF token unavailable'); return value; }
function mapError(error: unknown): never { if (error instanceof MenderApiError && error.status === 401) throw new ExecutionGovernanceLoginRequiredError(); throw error; }

function policy(item: Awaited<ReturnType<ReturnType<typeof createAdminGovernanceClient>['createExecutionPolicy']>>): ExecutionPolicyRevision { return { ...item }; }
function decision(item: Awaited<ReturnType<ReturnType<typeof createAdminGovernanceClient>['executionGovernance']>>['decisions'][number]): ExecutionPolicyDecision { return { ...item }; }
function confirmation(item: Awaited<ReturnType<ReturnType<typeof createAdminGovernanceClient>['executionGovernance']>>['confirmations'][number]): ExecutionConfirmation { return { ...item }; }

export function createExecutionGovernanceGateway(): ExecutionGovernanceGateway {
  const identity = createConsoleIdentityClient();
  const governance = createAdminGovernanceClient();
  return {
    async workspaces(signal) {
      try { return (await identity.listWorkspaces(signal)).map((item) => ({ id: item.workspaceId, role: item.role })); }
      catch (error) { return mapError(error); }
    },
    async snapshot(workspaceId, filter = {}, signal) {
      try {
        const item = await governance.executionGovernance(workspaceId, filter, signal);
        return {
          activePolicy: item.activePolicy ? policy(item.activePolicy) : null,
          revisions: item.revisions.map(policy), decisions: item.decisions.map(decision), confirmations: item.confirmations.map(confirmation),
          nextBeforeDecisionSequence: item.nextBeforeDecisionSequence, nextConfirmationCursor: item.nextConfirmationCursor,
        } satisfies ExecutionGovernanceSnapshot;
      } catch (error) { return mapError(error); }
    },
    async create(workspaceId, input, signal) {
      try { return policy(await governance.createExecutionPolicy(workspaceId, input, csrf(), signal)); }
      catch (error) { return mapError(error); }
    },
    async activate(workspaceId, policyId, signal) {
      try { return policy(await governance.activateExecutionPolicy(workspaceId, policyId, csrf(), signal)); }
      catch (error) { return mapError(error); }
    },
  };
}
