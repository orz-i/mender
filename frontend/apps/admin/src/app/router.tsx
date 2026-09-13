import { createBrowserRouter } from 'react-router';
import { HomePage } from './home-page';
import { Layout, NotFound, RouteError } from './route-pages';
import { StatusPage, createStatusReader } from '../modules/service-status';
import { PublicationReviewPage, createReviewGateway } from '../modules/publication-review';
import { PublicationHistoryPage, createHistoryGateway } from '../modules/publication-history';

const readStatus = createStatusReader();
const reviewGateway = createReviewGateway();
const historyGateway = createHistoryGateway();

export const router = createBrowserRouter([{
  element: <Layout />,
  errorElement: <RouteError />,
  children: [
    { index: true, element: <HomePage /> },
    { path: 'status', element: <StatusPage readStatus={readStatus} /> },
    { path: 'publication-reviews', element: <PublicationReviewPage gateway={reviewGateway} /> },
    { path: 'publication-history', element: <PublicationHistoryPage gateway={historyGateway} /> },
    { path: '*', element: <NotFound /> },
  ],
}]);
