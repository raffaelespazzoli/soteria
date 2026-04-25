import { useCallback, useMemo } from 'react';
import { useLocation, useNavigate } from 'react-router';

export interface FilterState {
  search: string;
  phase: string[];
  activeOn: string[];
  protected: string[];
  lastExecution: string[];
}

const EMPTY_FILTERS: FilterState = {
  search: '',
  phase: [],
  activeOn: [],
  protected: [],
  lastExecution: [],
};

function parseFiltersFromURL(searchString: string): FilterState {
  const params = new URLSearchParams(searchString);
  return {
    search: params.get('search') ?? '',
    phase: params.getAll('phase'),
    activeOn: params.getAll('activeOn'),
    protected: params.getAll('protected'),
    lastExecution: params.getAll('lastExecution'),
  };
}

function filtersToURLParams(filters: FilterState): string {
  const params = new URLSearchParams();
  if (filters.search) params.set('search', filters.search);
  filters.phase.forEach((v) => params.append('phase', v));
  filters.activeOn.forEach((v) => params.append('activeOn', v));
  filters.protected.forEach((v) => params.append('protected', v));
  filters.lastExecution.forEach((v) => params.append('lastExecution', v));
  return params.toString();
}

export function useFilterParams() {
  const location = useLocation();
  const navigate = useNavigate();

  const filters = useMemo(() => parseFiltersFromURL(location.search), [location.search]);

  const setFilters = useCallback(
    (newFilters: FilterState) => {
      const search = filtersToURLParams(newFilters);
      navigate({ search }, { replace: true });
    },
    [navigate],
  );

  const clearAllFilters = useCallback(() => {
    setFilters(EMPTY_FILTERS);
  }, [setFilters]);

  return { filters, setFilters, clearAllFilters };
}

export { EMPTY_FILTERS };
