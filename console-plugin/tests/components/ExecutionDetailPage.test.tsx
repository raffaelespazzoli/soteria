import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import ExecutionDetailPage from '../../src/components/ExecutionDetail/ExecutionDetailPage';
import { DRExecution, DRGroupResultValue } from '../../src/models/types';

expect.extend(toHaveNoViolations);

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  DocumentTitle: ({ children }: { children: React.ReactNode }) => (
    <title>{children}</title>
  ),
  k8sPatch: jest.fn(),
}));

jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useParams: () => ({ name: 'erp-failover-1714327200000' }),
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

const mockUseDRExecution = jest.fn<
  [DRExecution | undefined, boolean, unknown],
  [string]
>();

jest.mock('../../src/hooks/useDRResources', () => ({
  useDRExecution: (...args: [string]) => mockUseDRExecution(...args),
  useDRGroupStatuses: jest.fn().mockReturnValue([[], true, null]),
}));

const now = Date.now();

const mockActiveExecution: DRExecution = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRExecution',
  metadata: {
    name: 'erp-failover-1714327200000',
    uid: '1',
    labels: { 'soteria.io/plan-name': 'erp-full-stack' },
  },
  spec: { planName: 'erp-full-stack', mode: 'disaster' },
  status: {
    startTime: new Date(now - 4 * 60 * 1000).toISOString(),
    waves: [
      {
        waveIndex: 0,
        startTime: new Date(now - 4 * 60 * 1000).toISOString(),
        completionTime: new Date(now - 2.5 * 60 * 1000).toISOString(),
        groups: [
          {
            name: 'drgroup-1',
            result: DRGroupResultValue.Completed,
            vmNames: ['erp-db-1', 'erp-db-2'],
            startTime: new Date(now - 4 * 60 * 1000).toISOString(),
            completionTime: new Date(now - 2.5 * 60 * 1000).toISOString(),
          },
        ],
      },
      {
        waveIndex: 1,
        startTime: new Date(now - 2.5 * 60 * 1000).toISOString(),
        groups: [
          {
            name: 'drgroup-3',
            result: DRGroupResultValue.InProgress,
            vmNames: ['erp-app-1', 'erp-app-2', 'erp-app-3'],
            startTime: new Date(now - 2.5 * 60 * 1000).toISOString(),
          },
          {
            name: 'drgroup-4',
            result: DRGroupResultValue.Pending,
            vmNames: ['erp-app-4', 'erp-app-5'],
          },
        ],
      },
      {
        waveIndex: 2,
        groups: [
          {
            name: 'drgroup-5',
            result: DRGroupResultValue.Pending,
            vmNames: ['erp-web-1', 'erp-web-2', 'erp-web-3'],
          },
        ],
      },
    ],
  },
};

const mockCompletedExecution: DRExecution = {
  ...mockActiveExecution,
  status: {
    startTime: new Date(now - 10 * 60 * 1000).toISOString(),
    completionTime: new Date(now).toISOString(),
    result: 'Succeeded',
    rpoSeconds: 47,
    waves: [
      {
        waveIndex: 0,
        startTime: new Date(now - 10 * 60 * 1000).toISOString(),
        completionTime: new Date(now - 5 * 60 * 1000).toISOString(),
        groups: [
          {
            name: 'drgroup-1',
            result: DRGroupResultValue.Completed,
            vmNames: ['erp-db-1', 'erp-db-2'],
            startTime: new Date(now - 10 * 60 * 1000).toISOString(),
            completionTime: new Date(now - 5 * 60 * 1000).toISOString(),
          },
        ],
      },
      {
        waveIndex: 1,
        startTime: new Date(now - 5 * 60 * 1000).toISOString(),
        completionTime: new Date(now - 1 * 60 * 1000).toISOString(),
        groups: [
          {
            name: 'drgroup-3',
            result: DRGroupResultValue.Completed,
            vmNames: ['erp-app-1', 'erp-app-2', 'erp-app-3'],
            startTime: new Date(now - 5 * 60 * 1000).toISOString(),
            completionTime: new Date(now - 1 * 60 * 1000).toISOString(),
          },
        ],
      },
      {
        waveIndex: 2,
        startTime: new Date(now - 1 * 60 * 1000).toISOString(),
        completionTime: new Date(now).toISOString(),
        groups: [
          {
            name: 'drgroup-5',
            result: DRGroupResultValue.Completed,
            vmNames: ['erp-web-1', 'erp-web-2', 'erp-web-3'],
            startTime: new Date(now - 1 * 60 * 1000).toISOString(),
            completionTime: new Date(now).toISOString(),
          },
        ],
      },
    ],
  },
};

describe('ExecutionDetailPage', () => {
  afterEach(() => jest.restoreAllMocks());

  it('renders loading skeleton when execution is loading', () => {
    mockUseDRExecution.mockReturnValue([undefined, false, null]);
    render(<ExecutionDetailPage />);
    expect(screen.getByText('Loading execution details')).toBeInTheDocument();
  });

  it('renders error alert when execution fails to load', () => {
    mockUseDRExecution.mockReturnValue([undefined, true, new Error('Not found')]);
    render(<ExecutionDetailPage />);
    expect(screen.getByText(/Failed to load execution/)).toBeInTheDocument();
  });

  it('renders ProgressStepper with waves for active execution', () => {
    mockUseDRExecution.mockReturnValue([mockActiveExecution, true, null]);
    render(<ExecutionDetailPage />);
    expect(screen.getByLabelText('Execution wave progress')).toBeInTheDocument();
    expect(screen.getByText(/Wave 1/)).toBeInTheDocument();
    expect(screen.getByText(/Wave 2/)).toBeInTheDocument();
    expect(screen.getByText(/Wave 3/)).toBeInTheDocument();
  });

  it('renders execution header with name and mode', () => {
    mockUseDRExecution.mockReturnValue([mockActiveExecution, true, null]);
    render(<ExecutionDetailPage />);
    const nameMatches = screen.getAllByText('erp-failover-1714327200000');
    expect(nameMatches.length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('Disaster Failover')).toBeInTheDocument();
  });

  it('renders completed execution with result badge and duration', () => {
    mockUseDRExecution.mockReturnValue([mockCompletedExecution, true, null]);
    render(<ExecutionDetailPage />);
    expect(screen.getByText('Succeeded')).toBeInTheDocument();
    expect(screen.getByText(/Duration/)).toBeInTheDocument();
    expect(screen.getByText('RPO 47s')).toBeInTheDocument();
  });

  it('renders breadcrumb with plan name from execution spec', () => {
    mockUseDRExecution.mockReturnValue([mockActiveExecution, true, null]);
    render(<ExecutionDetailPage />);
    expect(screen.getByRole('navigation', { name: /breadcrumb/i })).toBeInTheDocument();
    const planLink = screen.getByText('erp-full-stack');
    expect(planLink).toBeInTheDocument();
    expect(planLink.closest('a')).toHaveAttribute(
      'href',
      '/disaster-recovery/plans/erp-full-stack',
    );
  });

  it('renders ARIA live region for screen readers', () => {
    mockUseDRExecution.mockReturnValue([mockActiveExecution, true, null]);
    const { container } = render(<ExecutionDetailPage />);
    const liveRegion = container.querySelector('[aria-live="polite"]');
    expect(liveRegion).toBeInTheDocument();
  });

  it('is the default export', () => {
    expect(ExecutionDetailPage).toBeDefined();
    expect(typeof ExecutionDetailPage).toBe('function');
  });

  it('has no accessibility violations (active execution)', async () => {
    mockUseDRExecution.mockReturnValue([mockActiveExecution, true, null]);
    const { container } = render(<ExecutionDetailPage />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations (completed execution)', async () => {
    mockUseDRExecution.mockReturnValue([mockCompletedExecution, true, null]);
    const { container } = render(<ExecutionDetailPage />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations (loading state)', async () => {
    mockUseDRExecution.mockReturnValue([undefined, false, null]);
    const { container } = render(<ExecutionDetailPage />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations (error state)', async () => {
    mockUseDRExecution.mockReturnValue([undefined, true, new Error('Not found')]);
    const { container } = render(<ExecutionDetailPage />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('calls useDRExecution with the name from URL params', () => {
    mockUseDRExecution.mockReturnValue([undefined, false, null]);
    render(<ExecutionDetailPage />);
    expect(mockUseDRExecution).toHaveBeenCalledWith('erp-failover-1714327200000');
  });

});

const mockPartialExecution: DRExecution = {
  ...mockActiveExecution,
  status: {
    startTime: new Date(now - 17 * 60 * 1000).toISOString(),
    completionTime: new Date(now).toISOString(),
    result: 'PartiallySucceeded',
    rpoSeconds: 47,
    waves: [
      {
        waveIndex: 0,
        startTime: new Date(now - 17 * 60 * 1000).toISOString(),
        completionTime: new Date(now - 10 * 60 * 1000).toISOString(),
        groups: [
          {
            name: 'drgroup-1',
            result: DRGroupResultValue.Completed,
            vmNames: ['erp-db-1', 'erp-db-2'],
            startTime: new Date(now - 17 * 60 * 1000).toISOString(),
            completionTime: new Date(now - 10 * 60 * 1000).toISOString(),
          },
        ],
      },
      {
        waveIndex: 1,
        startTime: new Date(now - 10 * 60 * 1000).toISOString(),
        completionTime: new Date(now - 2 * 60 * 1000).toISOString(),
        groups: [
          {
            name: 'drgroup-3',
            result: DRGroupResultValue.Failed,
            vmNames: ['erp-app-1', 'erp-app-2', 'erp-app-3'],
            error: 'storage driver timeout on SetSource',
            steps: [
              { name: 'StopReplication', status: 'Completed' },
              { name: 'StartVM', status: 'Failed', message: 'timeout after 5m' },
            ],
            retryCount: 0,
            startTime: new Date(now - 10 * 60 * 1000).toISOString(),
            completionTime: new Date(now - 2 * 60 * 1000).toISOString(),
          },
          {
            name: 'drgroup-4',
            result: DRGroupResultValue.Completed,
            vmNames: ['erp-app-4', 'erp-app-5'],
            startTime: new Date(now - 10 * 60 * 1000).toISOString(),
            completionTime: new Date(now - 3 * 60 * 1000).toISOString(),
          },
        ],
      },
      {
        waveIndex: 2,
        startTime: new Date(now - 2 * 60 * 1000).toISOString(),
        completionTime: new Date(now).toISOString(),
        groups: [
          {
            name: 'drgroup-5',
            result: DRGroupResultValue.Completed,
            vmNames: ['erp-web-1', 'erp-web-2', 'erp-web-3'],
            startTime: new Date(now - 2 * 60 * 1000).toISOString(),
            completionTime: new Date(now).toISOString(),
          },
        ],
      },
    ],
  },
};

const mockPartialWithRejection: DRExecution = {
  ...mockPartialExecution,
  status: {
    ...mockPartialExecution.status!,
    conditions: [
      { type: 'RetryRejected', status: 'True', message: 'VM erp-app-1 is in an unpredictable state — manual intervention required' },
    ],
  },
};

describe('ExecutionDetailPage — retry', () => {
  afterEach(() => jest.restoreAllMocks());

  it('renders retry buttons for PartiallySucceeded execution', () => {
    mockUseDRExecution.mockReturnValue([mockPartialExecution, true, null]);
    render(<ExecutionDetailPage />);
    expect(screen.getByRole('button', { name: /retry drgroup-3/i })).toBeInTheDocument();
  });

  it('shows error detail for failed group in PartiallySucceeded execution', () => {
    mockUseDRExecution.mockReturnValue([mockPartialExecution, true, null]);
    render(<ExecutionDetailPage />);
    expect(screen.getByText(/storage driver timeout/)).toBeInTheDocument();
    expect(screen.getByText('StartVM')).toBeInTheDocument();
  });

  it('renders PartiallySucceeded result badge', () => {
    mockUseDRExecution.mockReturnValue([mockPartialExecution, true, null]);
    render(<ExecutionDetailPage />);
    expect(screen.getByText('Partial')).toBeInTheDocument();
  });

  it('has no accessibility violations (PartiallySucceeded)', async () => {
    mockUseDRExecution.mockReturnValue([mockPartialExecution, true, null]);
    const { container } = render(<ExecutionDetailPage />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('displays RetryRejected condition message on page load', () => {
    mockUseDRExecution.mockReturnValue([mockPartialWithRejection, true, null]);
    render(<ExecutionDetailPage />);
    expect(
      screen.getByText(/VM erp-app-1 is in an unpredictable state/),
    ).toBeInTheDocument();
  });

  it('has no accessibility violations (PartiallySucceeded with rejection)', async () => {
    mockUseDRExecution.mockReturnValue([mockPartialWithRejection, true, null]);
    const { container } = render(<ExecutionDetailPage />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
