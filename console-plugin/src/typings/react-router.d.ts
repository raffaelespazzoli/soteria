/**
 * Type augmentation for react-router-dom.
 *
 * OCP 4.22+ ships React Router v6. The Console's shared module scope provides
 * `react-router`, `react-router-dom`, and `react-router-dom-v5-compat`.
 *
 * In React Router v6:
 *  - `react-router` exports: useNavigate, useLocation, useParams, useMatch, etc.
 *  - `react-router-dom` re-exports the above and adds: Link, NavLink, BrowserRouter, etc.
 *  - `useHistory` was removed; use `useNavigate` instead.
 *
 * All plugin imports should use `react-router-dom` for consistency and
 * to ensure DOM-specific components (Link, NavLink) resolve correctly.
 */
