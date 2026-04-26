import { render, screen, fireEvent } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import DRPlanDetailPage from '../../src/components/DRPlanDetail/DRPlanDetailPage';
import { DRPlan } from '../../src/models/types';

expect.extend(toHaveNoViolations);

const mockPlan: DRPlan = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRPlan',
  metadata: { name: 'erp-full-stack', uid: '1', creationTimestamp: '' },
  spec: {
    waveLabel: 'soteria.io/wave',
    maxConcurrentFailovers: 4,
    primarySite: 'dc1-prod',
    secondarySite: 'dc2-dr',
  },
  status: {
    phase: 'SteadyState',
    activeSite: 'dc1-prod',
    discoveredVMCount: 12,
    waves: [
      { waveKey: '1', vms: [{ name: 'vm1', namespace: 'ns1' }] },
      { waveKey: '2', vms: [{ name: 'vm2', namespace: 'ns1' }] },
      { waveKey: '3', vms: [{ name: 'vm3', namespace: 'ns1' }] },
    ],
    conditions: [
      { type: 'ReplicationHealthy', status: 'True', reason: 'Healthy', message: 'RPO: 12s' },
    ],
  },
};

const mockUseDRPlan = jest.fn<[DRPlan | undefined, boolean, unknown], [string]>();
const mockUseDRExecution = jest.fn();
const mockUseDRExecutions = jest.fn();

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  DocumentTitle: ({ children }: { children: React.ReactNode }) => <title>{children}</title>,
  useK8sWatchResource: jest.fn(() => [null, false, null]),
}));

jest.mock('../../src/hooks/useDRResources', () => ({
  useDRPlan: (...args: [string]) => mockUseDRPlan(...args),
  useDRExecution: (...args: unknown[]) => mockUseDRExecution(...args),
  useDRExecutions: (...args: unknown[]) => mockUseDRExecutions(...args),
}));

jest.mock('react-router', () => ({
  ...jest.requireActual('react-router'),
  useParams: () => ({ name: 'erp-full-stack' }),
  useNavigate: () => jest.fn(),
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

describe('DRPlanDetailPage', () => {
  beforeEach(() => {
    mockUseDRPlan.mockReturnValue([mockPlan, true, null]);
    mockUseDRExecution.mockReturnValue([undefined, true, null]);
    mockUseDRExecutions.mockReturnValue([[], true, null]);
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('renders breadcrumb with Disaster Recovery link', () => {
    render(<DRPlanDetailPage />);
    expect(screen.getByRole('link', { name: /disaster recovery/i })).toHaveAttribute(
      'href',
      '/disaster-recovery',
    );
  });

  it('renders the plan name in the breadcrumb and header', () => {
    render(<DRPlanDetailPage />);
    const matches = screen.getAllByText('erp-full-stack');
    expect(matches.length).toBeGreaterThanOrEqual(2);
  });

  it('renders four tabs: Overview, Waves, History, Configuration', () => {
    render(<DRPlanDetailPage />);
    expect(screen.getByRole('tab', { name: 'Overview' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Waves' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'History' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Configuration' })).toBeInTheDocument();
  });

  it('has Overview tab active by default', () => {
    render(<DRPlanDetailPage />);
    expect(screen.getByRole('tab', { name: 'Overview' })).toHaveAttribute('aria-selected', 'true');
  });

  it('renders plan header with VM count and wave count in Overview', () => {
    render(<DRPlanDetailPage />);
    expect(screen.getByText('12')).toBeInTheDocument();
    expect(screen.getByText('VMs')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('waves')).toBeInTheDocument();
  });

  it('renders DR lifecycle diagram in Overview tab', () => {
    render(<DRPlanDetailPage />);
    expect(screen.getByTestId('dr-lifecycle-diagram')).toBeInTheDocument();
  });

  it('renders real content for Waves, History, and Configuration tabs', () => {
    render(<DRPlanDetailPage />);
    expect(screen.getByLabelText('Wave composition')).toBeInTheDocument();
    expect(screen.getByText('No executions yet')).toBeInTheDocument();
    expect(screen.getByText('Max Concurrent Failovers')).toBeInTheDocument();
  });

  it('switches to Waves tab when clicked', () => {
    render(<DRPlanDetailPage />);
    fireEvent.click(screen.getByRole('tab', { name: 'Waves' }));
    expect(screen.getByRole('tab', { name: 'Waves' })).toHaveAttribute('aria-selected', 'true');
  });

  it('switches to History tab when clicked', () => {
    render(<DRPlanDetailPage />);
    fireEvent.click(screen.getByRole('tab', { name: 'History' }));
    expect(screen.getByRole('tab', { name: 'History' })).toHaveAttribute('aria-selected', 'true');
  });

  it('switches to Configuration tab when clicked', () => {
    render(<DRPlanDetailPage />);
    fireEvent.click(screen.getByRole('tab', { name: 'Configuration' }));
    expect(screen.getByRole('tab', { name: 'Configuration' })).toHaveAttribute('aria-selected', 'true');
  });

  it('renders loading skeleton when plan is not yet loaded', () => {
    mockUseDRPlan.mockReturnValue([undefined, false, null]);
    render(<DRPlanDetailPage />);
    expect(screen.getByText('Loading plan details')).toBeInTheDocument();
  });

  it('renders error alert when plan fetch fails', () => {
    mockUseDRPlan.mockReturnValue([undefined, true, new Error('Not found')]);
    render(<DRPlanDetailPage />);
    expect(screen.getByText('Failed to load DR plan')).toBeInTheDocument();
  });

  it('is the default export', () => {
    expect(DRPlanDetailPage).toBeDefined();
    expect(typeof DRPlanDetailPage).toBe('function');
  });

  it('has no accessibility violations', async () => {
    const { container } = render(<DRPlanDetailPage />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
