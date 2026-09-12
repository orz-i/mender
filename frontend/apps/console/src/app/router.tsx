import { createBrowserRouter } from 'react-router';
import { HomePage } from './home-page';
import { Layout, NotFound, RouteError } from './route-pages';
import { StatusPage, createStatusReader } from '../modules/service-status';
import { RunExplorerPage, createRunGateway } from '../modules/execution';
import { WorkspacesPage, createWorkspaceGateway } from '../modules/workspace-access';
import { ConnectionsPage, createConnectionGateway } from '../modules/connection-access';
import { ToolLaunchPage, createLaunchGateway } from '../modules/tool-launch';

const readStatus = createStatusReader();
const runGateway = createRunGateway();
const workspaceGateway = createWorkspaceGateway();
const connectionGateway = createConnectionGateway();
const launchGateway = createLaunchGateway();

export const router = createBrowserRouter([{
  element: <Layout />,
  errorElement: <RouteError />,
  children: [
    { index: true, element: <HomePage /> },
    { path: 'status', element: <StatusPage readStatus={readStatus} /> },
    { path: 'workspaces', element: <WorkspacesPage gateway={workspaceGateway} /> },
    { path: 'connections', element: <ConnectionsPage gateway={connectionGateway} /> },
    { path: 'launch', element: <ToolLaunchPage gateway={launchGateway} /> },
    { path: 'runs', element: <RunExplorerPage gateway={runGateway} /> },
    { path: '*', element: <NotFound /> },
  ],
}]);
