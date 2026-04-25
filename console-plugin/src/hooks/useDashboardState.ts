/**
 * Module-level state holder for DR Dashboard scroll position and filter state.
 * Survives component unmount/remount cycles during SPA navigation so that
 * returning to the dashboard (via breadcrumb or browser back) restores the
 * user's previous view. Intentionally kept outside the React component tree
 * to avoid Context provider boilerplate — the Console's routing model
 * guarantees a single DRDashboardPage instance at a time.
 */

export interface DashboardState {
  scrollTop: number;
  filters: Record<string, string[]>;
  searchText: string;
}

let savedState: DashboardState | null = null;

export function saveDashboardState(state: DashboardState): void {
  savedState = state;
}

export function restoreDashboardState(): DashboardState | null {
  return savedState;
}

export function clearDashboardState(): void {
  savedState = null;
}
