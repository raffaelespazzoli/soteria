/**
 * Type augmentation for react-router-dom.
 *
 * OCP 4.20 ships React Router v5. The Console's shared module scope provides
 * `react-router`, `react-router-dom`, and `react-router-dom-v5-compat`.
 *
 * In React Router v5:
 *  - `react-router` exports: useHistory, useLocation, useParams, useRouteMatch, etc.
 *  - `react-router-dom` re-exports the above and adds: Link, NavLink, BrowserRouter, etc.
 *
 * All plugin imports should use `react-router-dom` for consistency and
 * to ensure DOM-specific components (Link, NavLink) resolve correctly.
 */
