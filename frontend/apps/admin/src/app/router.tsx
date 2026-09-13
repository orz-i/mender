import { createBrowserRouter } from 'react-router';
import { HomePage } from './home-page';
import { Layout, NotFound, RouteError } from './route-pages';
import { StatusPage, createStatusReader } from '../modules/service-status';
import { PublicationReviewPage, createReviewGateway } from '../modules/publication-review';

const readStatus = createStatusReader();
const reviewGateway = createReviewGateway();

export const router = createBrowserRouter([{
  element: <Layout />,
  errorElement: <RouteError />,
  children: [
    { index: true, element: <HomePage /> },
    { path: 'status', element: <StatusPage readStatus={readStatus} /> },
    { path: 'publication-reviews', element: <PublicationReviewPage gateway={reviewGateway} /> },
    { path: '*', element: <NotFound /> },
  ],
}]);
