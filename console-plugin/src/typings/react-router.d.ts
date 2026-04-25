/**
 * Type augmentation for react-router.
 *
 * The Console shell provides React Router v7 at runtime where `react-router`
 * and `react-router-dom` are unified into a single package. Our local devDeps
 * still carry react-router v5 types. This declaration re-exports the
 * DOM-specific symbols so that source code can import everything from
 * `react-router` (the v7 convention) while still type-checking against v5.
 *
 * Runtime safety: webpack's ConsoleRemotePlugin configures `react-router` as
 * a shared module provided by the Console shell, so the local npm package is
 * never bundled into the plugin output. Jest tests mock `react-router`
 * entirely via jest.mock(), so the local package exports are irrelevant there
 * as well. This shim is therefore types-only by design.
 */
declare module 'react-router' {
  export {
    Link,
    NavLink,
    BrowserRouter,
    HashRouter,
    MemoryRouter,
    Route,
    Switch,
    useHistory,
    useLocation,
    useParams,
    useRouteMatch,
  } from 'react-router-dom';
}
