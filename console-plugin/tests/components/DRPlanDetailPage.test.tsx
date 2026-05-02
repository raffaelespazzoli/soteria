import { render, screen, fireEvent, act, waitFor } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import DRPlanDetailPage from '../../src/components/DRPlanDetail/DRPlanDetailPage';
import { DRPlan } from '../../src/models/types';

expect.extend(toHaveNoViolations);

const mockPlan: DRPlan = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRPlan',
  metadata: { name: 'erp-full-stack', uid: '1', creationTimestamp: '' },
  spec: {
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
const mockUseDRExecutions = jest.fn();

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  DocumentTitle: ({ children }: { children: React.ReactNode }) => <title>{children}</title>,
  useK8sWatchResource: jest.fn(() => [null, false, null]),
}));

jest.mock('../../src/hooks/useDRResources', () => ({
  useDRPlan: (...args: [string]) => mockUseDRPlan(...args),
  useDRExecutions: (...args: unknown[]) => mockUseDRExecutions(...args),
}));

const mockCreate = jest.fn();
const mockClearError = jest.fn();
jest.mock('../../src/hooks/useCreateDRExecution', () => ({
  useCreateDRExecution: () => ({
    create: mockCreate,
    isCreating: false,
    error: undefined,
    clearError: mockClearError,
  }),
}));

jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useParams: () => ({ name: 'erp-full-stack' }),
  useHistory: () => ({ push: jest.fn(), replace: jest.fn(), location: { search: '' } }),
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

beforeAll(() => {
  Element.prototype.scrollIntoView = jest.fn();
});

describe('DRPlanDetailPage', () => {
  beforeEach(() => {
    mockUseDRPlan.mockReturnValue([mockPlan, true, null]);
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

  describe('SiteDisagreementAlert integration', () => {
    const planWithSitesOutOfSync: DRPlan = {
      ...mockPlan,
      status: {
        ...mockPlan.status,
        conditions: [
          { type: 'ReplicationHealthy', status: 'True', reason: 'Healthy', message: 'RPO: 12s' },
          {
            type: 'SitesInSync',
            status: 'False',
            reason: 'VMsMismatch',
            message: 'VMs on primary but not secondary: [ns1/vm-a]; VMs on secondary but not primary: [ns1/vm-b]',
          },
        ],
      },
    };

    it('renders danger alert when plan has SitesInSync=False', () => {
      mockUseDRPlan.mockReturnValue([planWithSitesOutOfSync, true, null]);
      render(<DRPlanDetailPage />);
      expect(
        screen.getByText('Sites do not agree on VM inventory — DR operations are blocked'),
      ).toBeInTheDocument();
    });

    it('clicking "View site differences" switches to Configuration tab', () => {
      mockUseDRPlan.mockReturnValue([planWithSitesOutOfSync, true, null]);
      render(<DRPlanDetailPage />);
      fireEvent.click(screen.getByText('View site differences'));
      expect(screen.getByRole('tab', { name: 'Configuration' })).toHaveAttribute(
        'aria-selected',
        'true',
      );
    });

    it('renders no alert when plan has SitesInSync=True', () => {
      const planInSync: DRPlan = {
        ...mockPlan,
        status: {
          ...mockPlan.status,
          conditions: [
            { type: 'ReplicationHealthy', status: 'True', reason: 'Healthy', message: 'RPO: 12s' },
            { type: 'SitesInSync', status: 'True', reason: 'VMsAgreed' },
          ],
        },
      };
      mockUseDRPlan.mockReturnValue([planInSync, true, null]);
      render(<DRPlanDetailPage />);
      expect(
        screen.queryByText('Sites do not agree on VM inventory — DR operations are blocked'),
      ).not.toBeInTheDocument();
    });
  });

  describe('optimistic execution state', () => {
    beforeEach(() => {
      mockCreate.mockReset();
    });

    async function triggerFailoverConfirm() {
      fireEvent.click(screen.getByRole('button', { name: 'Failover' }));
      const keywordInput = screen.getByLabelText('Type FAILOVER to confirm');
      fireEvent.change(keywordInput, { target: { value: 'FAILOVER' } });
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: 'Confirm Failover' }));
      });
    }

    it('shows optimistic banner immediately after successful create', async () => {
      mockCreate.mockResolvedValue({
        apiVersion: 'soteria.io/v1alpha1',
        kind: 'DRExecution',
        metadata: { name: 'erp-full-stack-failover-123', uid: 'uid-1', creationTimestamp: '' },
        spec: { planName: 'erp-full-stack', mode: 'disaster' },
      });
      render(<DRPlanDetailPage />);
      await triggerFailoverConfirm();

      expect(screen.getByText('Starting Failover...')).toBeInTheDocument();
      expect(screen.getByLabelText('Execution starting')).toBeInTheDocument();
    });

    it('replaces optimistic banner with real data when activeExecution arrives', async () => {
      mockCreate.mockResolvedValue({
        apiVersion: 'soteria.io/v1alpha1',
        kind: 'DRExecution',
        metadata: { name: 'erp-full-stack-failover-123', uid: 'uid-1', creationTimestamp: '' },
        spec: { planName: 'erp-full-stack', mode: 'disaster' },
      });
      const { rerender } = render(<DRPlanDetailPage />);
      await triggerFailoverConfirm();
      expect(screen.getByText('Starting Failover...')).toBeInTheDocument();

      const updatedPlan: DRPlan = {
        ...mockPlan,
        status: {
          ...mockPlan.status,
          activeExecution: 'erp-full-stack-failover-123',
          activeExecutionMode: 'disaster',
        },
      };
      const exec = {
        apiVersion: 'soteria.io/v1alpha1',
        kind: 'DRExecution',
        metadata: { name: 'erp-full-stack-failover-123', uid: 'uid-1', creationTimestamp: '' },
        spec: { planName: 'erp-full-stack', mode: 'disaster' as const },
        status: {
          startTime: new Date().toISOString(),
          waves: [{ waveIndex: 0 }, { waveIndex: 1 }],
        },
      };
      mockUseDRPlan.mockReturnValue([updatedPlan, true, null]);
      mockUseDRExecutions.mockReturnValue([[exec], true, null]);

      await act(async () => {
        rerender(<DRPlanDetailPage />);
      });

      expect(screen.queryByText('Starting Failover...')).not.toBeInTheDocument();
      expect(screen.getByText('Failing Over in progress')).toBeInTheDocument();
    });

    it('does not show optimistic banner on create failure', async () => {
      mockCreate.mockRejectedValue(new Error('concurrent execution already active'));
      render(<DRPlanDetailPage />);

      fireEvent.click(screen.getByRole('button', { name: 'Failover' }));
      const keywordInput = screen.getByLabelText('Type FAILOVER to confirm');
      fireEvent.change(keywordInput, { target: { value: 'FAILOVER' } });
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: 'Confirm Failover' }));
      });

      expect(screen.queryByText(/Starting Failover/)).not.toBeInTheDocument();
    });

    it('clears optimistic state after 30s safety timeout', async () => {
      jest.useFakeTimers();
      mockCreate.mockResolvedValue({
        apiVersion: 'soteria.io/v1alpha1',
        kind: 'DRExecution',
        metadata: { name: 'erp-full-stack-failover-123', uid: 'uid-1', creationTimestamp: '' },
        spec: { planName: 'erp-full-stack', mode: 'disaster' },
      });
      render(<DRPlanDetailPage />);
      await triggerFailoverConfirm();
      expect(screen.getByText('Starting Failover...')).toBeInTheDocument();

      act(() => {
        jest.advanceTimersByTime(30_000);
      });

      expect(screen.queryByText('Starting Failover...')).not.toBeInTheDocument();
      jest.useRealTimers();
    });

    it('optimistic state is not persisted across unmount/remount', async () => {
      mockCreate.mockResolvedValue({
        apiVersion: 'soteria.io/v1alpha1',
        kind: 'DRExecution',
        metadata: { name: 'erp-full-stack-failover-123', uid: 'uid-1', creationTimestamp: '' },
        spec: { planName: 'erp-full-stack', mode: 'disaster' },
      });
      const { unmount } = render(<DRPlanDetailPage />);
      await triggerFailoverConfirm();
      expect(screen.getByText('Starting Failover...')).toBeInTheDocument();

      unmount();
      render(<DRPlanDetailPage />);
      expect(screen.queryByText('Starting Failover...')).not.toBeInTheDocument();
    });

    it('has no accessibility violations with optimistic banner', async () => {
      mockCreate.mockResolvedValue({
        apiVersion: 'soteria.io/v1alpha1',
        kind: 'DRExecution',
        metadata: { name: 'erp-full-stack-failover-123', uid: 'uid-1', creationTimestamp: '' },
        spec: { planName: 'erp-full-stack', mode: 'disaster' },
      });
      const { container } = render(<DRPlanDetailPage />);
      await triggerFailoverConfirm();
      expect(screen.getByText('Starting Failover...')).toBeInTheDocument();

      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });
  });
});
