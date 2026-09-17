import { createBrowserRouter } from 'react-router';
import { HomePage } from './home-page';
import { Layout, NotFound, RouteError } from './route-pages';
import { StatusPage, createStatusReader } from '../modules/service-status';
import { RunExplorerPage, createRunGateway } from '../modules/execution';
import { WorkspacesPage, createWorkspaceGateway } from '../modules/workspace-access';
import { ConnectionsPage, createConnectionGateway } from '../modules/connection-access';
import { ToolLaunchPage, createLaunchGateway } from '../modules/tool-launch';
import { UsagePage, createUsageGateway } from '../modules/usage-observability';
import { CatalogManagementPage, ToolCatalogPage, createCatalogGateway } from '../modules/catalog-management';

const readStatus = createStatusReader();
const runGateway = createRunGateway();
const workspaceGateway = createWorkspaceGateway();
const connectionGateway = createConnectionGateway();
const launchGateway = createLaunchGateway();
const usageGateway = createUsageGateway();
const catalogGateway = createCatalogGateway();

export const router = createBrowserRouter([{
  element: <Layout />,
  errorElement: <RouteError />,
  children: [
    { index: true, element: <HomePage /> },
    { path: 'status', element: <StatusPage readStatus={readStatus} /> },
    { path: 'workspaces', element: <WorkspacesPage gateway={workspaceGateway} /> },
    { path: 'connections', element: <ConnectionsPage gateway={connectionGateway} /> },
    { path: 'catalog', element: <ToolCatalogPage gateway={catalogGateway} /> },
    { path: 'launch', element: <ToolLaunchPage gateway={launchGateway} /> },
    { path: 'usage', element: <UsagePage gateway={usageGateway} /> },
    { path: 'runs', element: <RunExplorerPage gateway={runGateway} /> },
    { path: 'publisher', lazy: async () => {
      const { PublisherWorkbenchPage, createPublisherWorkbenchGateway } = await import('../modules/publisher-workbench');
      const gateway = createPublisherWorkbenchGateway();
      return { Component: function PublisherRoute() { return <PublisherWorkbenchPage gateway={gateway} />; } };
    } },
    { path: 'publisher/catalog', element: <CatalogManagementPage gateway={catalogGateway} /> },
    { path: '*', element: <NotFound /> },
  ],
}]);
