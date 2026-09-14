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
    { path: '*', element: <NotFound /> },
  ],
}]);
