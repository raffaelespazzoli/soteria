import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import PhaseBadge from '../../src/components/shared/PhaseBadge';
import ExecutionResultBadge from '../../src/components/shared/ExecutionResultBadge';
import ReplicationHealthIndicator from '../../src/components/shared/ReplicationHealthIndicator';
import { DashboardEmptyState } from '../../src/components/DRDashboard/DashboardEmptyState';
import DRDashboard from '../../src/components/DRDashboard/DRDashboard';
import AlertBannerSystem from '../../src/components/DRDashboard/AlertBannerSystem';
import DRLifecycleDiagram from '../../src/components/DRPlanDetail/DRLifecycleDiagram';
import { WaveCompositionTree } from '../../src/components/DRPlanDetail/WaveCompositionTree';
import { ExecutionHistoryTable } from '../../src/components/DRPlanDetail/ExecutionHistoryTable';
import { PlanConfiguration } from '../../src/components/DRPlanDetail/PlanConfiguration';
import { ReplicationHealthExpanded } from '../../src/components/DRPlanDetail/ReplicationHealthExpanded';
import { DRPlan, DRExecution, DRExecutionResult } from '../../src/models/types';
import { EffectivePhase, ReplicationHealthStatus } from '../../src/utils/drPlanUtils';

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

const mockSteadyStatePlan: DRPlan = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRPlan',
  metadata: {
    name: 'erp-full-stack',
    uid: '1',
    creationTimestamp: '2026-04-02T10:00:00Z',
    labels: { 'app.kubernetes.io/part-of': 'erp-system' },
    annotations: { 'soteria.io/description': 'ERP DR plan' },
  },
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
      {
        waveKey: '1',
        vms: [{ name: 'erp-db-1', namespace: 'erp-db' }],
        groups: [
          { name: 'drgroup-1', namespace: 'erp-db', consistencyLevel: 'vm', vmNames: ['erp-db-1'] },
        ],
      },
    ],
    replicationHealth: [
      { name: 'drgroup-1', namespace: 'erp-db', health: 'Healthy', lastChecked: '2026-04-25T15:00:00Z' },
    ],
    conditions: [
      { type: 'ReplicationHealthy', status: 'True', reason: 'Healthy', message: 'RPO: 12s', lastTransitionTime: '2026-04-25T15:00:00Z' },
    ],
  },
};

const mockFailingOverPlan: DRPlan = {
  ...mockSteadyStatePlan,
  status: {
    ...mockSteadyStatePlan.status!,
    activeExecution: 'erp-full-stack-failover-001',
    activeExecutionMode: 'disaster',
  },
};

const mockBrokenPlan: DRPlan = {
  ...mockSteadyStatePlan,
  metadata: { ...mockSteadyStatePlan.metadata!, name: 'broken-plan', uid: '99' },
  status: {
    ...mockSteadyStatePlan.status!,
    conditions: [
      { type: 'ReplicationHealthy', status: 'False', reason: 'Error', message: 'Replication broken', lastTransitionTime: '2026-04-25T15:00:00Z' },
    ],
  },
};

const mockDegradedPlan: DRPlan = {
  ...mockSteadyStatePlan,
  metadata: { ...mockSteadyStatePlan.metadata!, name: 'degraded-plan', uid: '98' },
  status: {
    ...mockSteadyStatePlan.status!,
    conditions: [
      { type: 'ReplicationHealthy', status: 'False', reason: 'Degraded', message: 'RPO: 120s', lastTransitionTime: '2026-04-25T15:00:00Z' },
    ],
  },
};

const mockExecutions: DRExecution[] = [
  {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRExecution',
    metadata: { name: 'exec-1', uid: '10', creationTimestamp: '', annotations: { 'soteria.io/triggered-by': 'admin' } },
    spec: { planName: 'erp-full-stack', mode: 'planned_migration' },
    status: {
      result: 'Succeeded',
      startTime: '2026-04-24T10:00:00Z',
      completionTime: '2026-04-24T10:05:00Z',
    },
  },
  {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRExecution',
    metadata: { name: 'exec-2', uid: '11', creationTimestamp: '' },
    spec: { planName: 'erp-full-stack', mode: 'disaster' },
    status: {
      result: 'Failed',
      startTime: '2026-04-23T08:00:00Z',
      completionTime: '2026-04-23T08:15:00Z',
    },
  },
];

let mockPlansData: DRPlan[] = [mockSteadyStatePlan];

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  useK8sWatchResource: jest.fn(
    (resource: { groupVersionKind?: { kind?: string } }) => {
      if (resource?.groupVersionKind?.kind === 'DRExecution') return [mockExecutions, true, null];
      return [mockPlansData, true, null];
    },
  ),
}));

const ALL_PHASES: EffectivePhase[] = [
  'SteadyState', 'DRedSteadyState', 'FailedOver', 'FailedBack',
  'FailingOver', 'Reprotecting', 'FailingBack', 'Restoring',
];

const ALL_RESULTS: DRExecutionResult[] = ['Succeeded', 'PartiallySucceeded', 'Failed'];

const ALL_HEALTH_STATES: ReplicationHealthStatus[] = ['Healthy', 'Degraded', 'Syncing', 'Error', 'Unknown'];

describe('Accessibility audit — PhaseBadge', () => {
  it.each(ALL_PHASES)(
    '%s passes jest-axe',
    async (phase) => {
      const { container } = render(<PhaseBadge phase={phase} />);
      expect(await axe(container)).toHaveNoViolations();
    },
  );
});

describe('Accessibility audit — ExecutionResultBadge', () => {
  it.each(ALL_RESULTS)(
    '%s passes jest-axe',
    async (result) => {
      const { container } = render(<ExecutionResultBadge result={result} />);
      expect(await axe(container)).toHaveNoViolations();
    },
  );
});

describe('Accessibility audit — ReplicationHealthIndicator', () => {
  it.each(ALL_HEALTH_STATES)(
    '%s passes jest-axe',
    async (healthStatus) => {
      const { container } = render(
        <ReplicationHealthIndicator health={{ status: healthStatus }} />,
      );
      expect(await axe(container)).toHaveNoViolations();
    },
  );
});

describe('Accessibility audit — DashboardEmptyState', () => {
  it('passes jest-axe', async () => {
    const { container } = render(<DashboardEmptyState />);
    expect(await axe(container)).toHaveNoViolations();
  });
});

describe('Accessibility audit — DRDashboard', () => {
  it('DRDashboard with plans passes jest-axe', async () => {
    mockPlansData = [mockSteadyStatePlan];
    const { container } = render(<DRDashboard />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it('DRDashboard empty passes jest-axe', async () => {
    mockPlansData = [];
    const { container } = render(<DRDashboard />);
    expect(await axe(container)).toHaveNoViolations();
    mockPlansData = [mockSteadyStatePlan];
  });

  it('DRDashboard empty state renders "No DR Plans configured"', () => {
    mockPlansData = [];
    render(<DRDashboard />);
    expect(screen.getByText('No DR Plans configured')).toBeInTheDocument();
    expect(screen.queryByRole('grid')).not.toBeInTheDocument();
    mockPlansData = [mockSteadyStatePlan];
  });
});

describe('Accessibility audit — AlertBannerSystem', () => {
  it('danger banner passes jest-axe', async () => {
    const { container } = render(
      <AlertBannerSystem plans={[mockBrokenPlan]} onFilterByHealth={jest.fn()} />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it('warning banner passes jest-axe', async () => {
    const { container } = render(
      <AlertBannerSystem plans={[mockDegradedPlan]} onFilterByHealth={jest.fn()} />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it('no banners passes jest-axe', async () => {
    const { container } = render(
      <AlertBannerSystem plans={[mockSteadyStatePlan]} onFilterByHealth={jest.fn()} />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });
});

describe('Accessibility audit — DRLifecycleDiagram', () => {
  it('rest state (SteadyState) passes jest-axe', async () => {
    const { container } = render(
      <DRLifecycleDiagram plan={mockSteadyStatePlan} onAction={jest.fn()} />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it('transient state (FailingOver) passes jest-axe', async () => {
    const { container } = render(
      <DRLifecycleDiagram plan={mockFailingOverPlan} onAction={jest.fn()} />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });
});

describe('Accessibility audit — WaveCompositionTree', () => {
  it('passes jest-axe with waves', async () => {
    const { container } = render(<WaveCompositionTree plan={mockSteadyStatePlan} />);
    expect(await axe(container)).toHaveNoViolations();
  });
});

describe('Accessibility audit — ExecutionHistoryTable', () => {
  it('with data passes jest-axe', async () => {
    const { container } = render(
      <ExecutionHistoryTable executions={mockExecutions} planName="erp-full-stack" />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it('empty passes jest-axe', async () => {
    const { container } = render(
      <ExecutionHistoryTable executions={[]} planName="erp-full-stack" />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });
});

describe('Accessibility audit — PlanConfiguration', () => {
  it('passes jest-axe', async () => {
    const { container } = render(<PlanConfiguration plan={mockSteadyStatePlan} />);
    expect(await axe(container)).toHaveNoViolations();
  });
});

describe('Accessibility audit — ReplicationHealthExpanded', () => {
  it('with health data passes jest-axe', async () => {
    const { container } = render(<ReplicationHealthExpanded plan={mockSteadyStatePlan} />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it('without health data passes jest-axe', async () => {
    const planNoHealth: DRPlan = {
      ...mockSteadyStatePlan,
      status: { ...mockSteadyStatePlan.status!, replicationHealth: [] },
    };
    const { container } = render(<ReplicationHealthExpanded plan={planNoHealth} />);
    expect(await axe(container)).toHaveNoViolations();
  });
});
