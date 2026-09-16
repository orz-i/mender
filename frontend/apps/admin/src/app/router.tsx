import { BillingWorkbenchPage, createBillingWorkbenchGateway } from '../modules/billing-workbench';
import { SupportApprovalsPage, createSupportApprovalsGateway } from '../modules/support-approvals';
import { PlatformOperationsPage, createPlatformOperationsGateway } from '../modules/platform-operations';
import { ReleaseWorkbenchPage, createReleaseWorkbenchGateway } from '../modules/release-workbench';
import { createBrowserRouter } from 'react-router';
import { HomePage } from './home-page';
import { Layout, NotFound, RouteError } from './route-pages';
import { StatusPage, createStatusReader } from '../modules/service-status';
import { PublicationReviewPage, createReviewGateway } from '../modules/publication-review';
import { PublicationHistoryPage, createHistoryGateway } from '../modules/publication-history';
import { PublicationPolicyPage, createPolicyGateway } from '../modules/publication-policy';
import { ExecutionGovernancePage, createExecutionGovernanceGateway } from '../modules/execution-governance';

const readStatus = createStatusReader();
const reviewGateway = createReviewGateway();
const historyGateway = createHistoryGateway();
const policyGateway = createPolicyGateway();
const executionGovernanceGateway = createExecutionGovernanceGateway();

const releaseWorkbenchPageGateway = createReleaseWorkbenchGateway();

const platformOperationsPageGateway = createPlatformOperationsGateway();

const supportApprovalsPageGateway = createSupportApprovalsGateway();

const billingWorkbenchPageGateway = createBillingWorkbenchGateway();

export const router = createBrowserRouter([{
  element: <Layout />,
  errorElement: <RouteError />,
  children: [
    { index: true, element: <HomePage /> },
    { path: 'status', element: <StatusPage readStatus={readStatus} /> },
    { path: 'publication-reviews', element: <PublicationReviewPage gateway={reviewGateway} /> },
    { path: 'publication-history', element: <PublicationHistoryPage gateway={historyGateway} /> },
    { path: 'publication-policy', element: <PublicationPolicyPage gateway={policyGateway} /> },
    { path: 'execution-governance', element: <ExecutionGovernancePage gateway={executionGovernanceGateway} /> },
    { path: 'release-management', element: <ReleaseWorkbenchPage gateway={releaseWorkbenchPageGateway} /> },
    { path: 'platform-operations', element: <PlatformOperationsPage gateway={platformOperationsPageGateway} /> },
    { path: 'support-approvals', element: <SupportApprovalsPage gateway={supportApprovalsPageGateway} /> },
    { path: 'billing', element: <BillingWorkbenchPage gateway={billingWorkbenchPageGateway} /> },
    { path: '*', element: <NotFound /> },
  ],
}]);
