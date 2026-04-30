import { useParams } from 'react-router-dom';

/**
 * Extract the :name route parameter using multiple strategies.
 *
 * The OpenShift Console plugin SDK renders console.page/route components
 * outside a React Router <Route> context, so useParams() returns undefined.
 * We fall back through: match prop -> useParams -> pathname extraction.
 */
export function useRouteParamName(
  match?: { params?: { name?: string } },
): string | undefined {
  const routerParams = useParams<{ name: string }>();
  return match?.params?.name ?? routerParams?.name ?? window.location.pathname.split('/').pop() ?? undefined;
}
