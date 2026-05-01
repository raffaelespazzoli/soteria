import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import DRDashboard from '../../src/components/DRDashboard/DRDashboard';
import { DRPlan, DRExecution } from '../../src/models/types';

expect.extend(toHaveNoViolations);

const mockNavigate = jest.fn();

jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useLocation: () => ({ search: '', pathname: '/disaster-recovery' }),
  useHistory: () => ({ replace: mockNavigate, push: mockNavigate, location: { search: '' } }),
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

const mockPlans: DRPlan[] = [
  {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRPlan',
    metadata: { name: 'plan-alpha', uid: '1', creationTimestamp: '' },
    spec: {
      maxConcurrentFailovers: 1,
      primarySite: 'site-a',
      secondarySite: 'site-b',
    },
    status: {
      phase: 'SteadyState',
      activeSite: 'site-a',
      conditions: [
        {
          type: 'ReplicationHealthy',
          status: 'True',
          message: 'RPO: 12s',
        },
      ],
    },
  },
  {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRPlan',
    metadata: { name: 'plan-beta', uid: '2', creationTimestamp: '' },
    spec: {
      maxConcurrentFailovers: 1,
      primarySite: 'site-a',
      secondarySite: 'site-b',
    },
    status: {
      phase: 'FailedOver',
      activeSite: 'site-b',
      conditions: [
        {
          type: 'ReplicationHealthy',
          status: 'False',
          reason: 'Error',
          message: 'storage driver unreachable',
        },
      ],
    },
  },
];

const mockExecutions: DRExecution[] = [
  {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRExecution',
    metadata: { name: 'exec-1', uid: '10', creationTimestamp: '' },
    spec: { planName: 'plan-alpha', mode: 'planned_migration' },
    status: {
      result: 'Succeeded',
      startTime: '2026-04-24T10:00:00Z',
      completionTime: '2026-04-24T10:05:00Z',
    },
  },
];

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  useK8sWatchResource: jest.fn(
    (resource: { groupVersionKind?: { kind?: string }; isList?: boolean }) => {
      if (resource.groupVersionKind?.kind === 'DRExecution')
        return [mockExecutions, true, null];
      return [mockPlans, true, null];
    },
  ),
  DocumentTitle: ({ children }: { children: React.ReactNode }) => <title>{children}</title>,
}));

describe('DRDashboard', () => {
  beforeEach(() => {
    mockNavigate.mockClear();
  });

  it('renders the table with column headers', () => {
    render(<DRDashboard />);
    const table = screen.getByRole('grid');
    expect(table).toBeInTheDocument();
    const columnHeaders = screen.getAllByRole('columnheader');
    const headerTexts = columnHeaders.map((h) => h.textContent?.trim());
    expect(headerTexts).toEqual(
      expect.arrayContaining(['Name', 'Phase', 'Active On', 'Protected', 'Last Execution', 'Actions']),
    );
  });

  it('renders plan names as links to detail pages', () => {
    render(<DRDashboard />);
    const link = screen.getByRole('link', { name: 'plan-alpha' });
    expect(link).toHaveAttribute('href', '/disaster-recovery/plans/plan-alpha');
  });

  it('renders phase badges for each plan', () => {
    render(<DRDashboard />);
    expect(screen.getByText('Steady State')).toBeInTheDocument();
    expect(screen.getByText('Failed Over')).toBeInTheDocument();
  });

  it('renders active site for each plan', () => {
    render(<DRDashboard />);
    expect(screen.getByText('site-a')).toBeInTheDocument();
    expect(screen.getByText('site-b')).toBeInTheDocument();
  });

  it('renders replication health indicators', () => {
    render(<DRDashboard />);
    expect(screen.getByText('Healthy')).toBeInTheDocument();
    expect(screen.getByText('Error')).toBeInTheDocument();
  });

  it('renders last execution info', () => {
    render(<DRDashboard />);
    expect(screen.getByText('Succeeded')).toBeInTheDocument();
    expect(screen.getByText('Never')).toBeInTheDocument();
  });

  it('shows plan count in toolbar', () => {
    render(<DRDashboard />);
    expect(screen.getByText(/showing 2 of 2 plans/i)).toBeInTheDocument();
  });

  it('default-sorts by Protected column (worst-first)', () => {
    render(<DRDashboard />);
    const rows = screen.getAllByRole('row');
    // Row 0 is header, row 1 is first data row (should be Error = plan-beta)
    const firstDataRow = rows[1];
    expect(firstDataRow).toHaveTextContent('plan-beta');
  });

  it('has no accessibility violations', async () => {
    const { container } = render(<DRDashboard />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
