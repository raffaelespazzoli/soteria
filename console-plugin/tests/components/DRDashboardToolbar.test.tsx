import { render, screen, fireEvent } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import DRDashboardToolbar from '../../src/components/DRDashboard/DRDashboardToolbar';
import { FilterState } from '../../src/hooks/useFilterParams';
import { DRPlan } from '../../src/models/types';

expect.extend(toHaveNoViolations);

const emptyFilters: FilterState = {
  search: '',
  phase: [],
  activeOn: [],
  protected: [],
  lastExecution: [],
};

const mockPlans: DRPlan[] = [
  {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRPlan',
    metadata: { name: 'plan-1', uid: '1', creationTimestamp: '' },
    spec: { maxConcurrentFailovers: 1, primarySite: 'a', secondarySite: 'b' },
    status: { activeSite: 'site-a' },
  },
  {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRPlan',
    metadata: { name: 'plan-2', uid: '2', creationTimestamp: '' },
    spec: { maxConcurrentFailovers: 1, primarySite: 'a', secondarySite: 'b' },
    status: { activeSite: 'site-b' },
  },
];

describe('DRDashboardToolbar', () => {
  const onFiltersChange = jest.fn();
  const onClearAll = jest.fn();

  beforeEach(() => {
    onFiltersChange.mockClear();
    onClearAll.mockClear();
  });

  function renderToolbar(filters: FilterState = emptyFilters) {
    return render(
      <DRDashboardToolbar
        filters={filters}
        onFiltersChange={onFiltersChange}
        onClearAll={onClearAll}
        plans={mockPlans}
        filteredCount={2}
        totalCount={2}
      />,
    );
  }

  it('renders search input', () => {
    renderToolbar();
    expect(screen.getByPlaceholderText('Filter by name...')).toBeInTheDocument();
  });

  it('renders filter dropdowns', () => {
    renderToolbar();
    expect(screen.getByText('Phase')).toBeInTheDocument();
    expect(screen.getByText('Active On')).toBeInTheDocument();
    expect(screen.getByText('Protected')).toBeInTheDocument();
    expect(screen.getByText('Last Execution')).toBeInTheDocument();
  });

  it('shows plan count', () => {
    renderToolbar();
    expect(screen.getByText('Showing 2 of 2 plans')).toBeInTheDocument();
  });

  it('calls onFiltersChange when search text changes', () => {
    renderToolbar();
    const input = screen.getByPlaceholderText('Filter by name...');
    fireEvent.change(input, { target: { value: 'test' } });
    expect(onFiltersChange).toHaveBeenCalledWith(
      expect.objectContaining({ search: 'test' }),
    );
  });

  it('shows active filter labels as badges when filters are set', () => {
    renderToolbar({ ...emptyFilters, phase: ['SteadyState'] });
    expect(screen.getByText('SteadyState')).toBeInTheDocument();
  });

  it('has no accessibility violations', async () => {
    const { container } = renderToolbar();
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
