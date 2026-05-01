import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import DRDashboard from '../../src/components/DRDashboard/DRDashboard';
import DRLifecycleDiagram from '../../src/components/DRPlanDetail/DRLifecycleDiagram';
import { WaveCompositionTree } from '../../src/components/DRPlanDetail/WaveCompositionTree';
import { DRPlan, DRExecution } from '../../src/models/types';

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
    conditions: [
      { type: 'ReplicationHealthy', status: 'True', reason: 'Healthy', message: 'RPO: 12s' },
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

const mockDRedSteadyStatePlan: DRPlan = {
  ...mockSteadyStatePlan,
  status: { ...mockSteadyStatePlan.status!, phase: 'DRedSteadyState' },
};

const mockPlans: DRPlan[] = [mockSteadyStatePlan];
const mockExecs: DRExecution[] = [];

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  useK8sWatchResource: jest.fn(
    (resource: { groupVersionKind?: { kind?: string } }) => {
      if (resource?.groupVersionKind?.kind === 'DRExecution') return [mockExecs, true, null];
      return [mockPlans, true, null];
    },
  ),
}));

const mockPlanWithWaves: DRPlan = {
  ...mockSteadyStatePlan,
  status: {
    ...mockSteadyStatePlan.status!,
    waves: [
      {
        waveKey: '1',
        vms: [{ name: 'erp-db-1', namespace: 'erp-db' }],
        groups: [
          { name: 'drgroup-1', namespace: 'erp-db', consistencyLevel: 'vm' as const, vmNames: ['erp-db-1'] },
        ],
      },
      {
        waveKey: '2',
        vms: [{ name: 'erp-app-1', namespace: 'erp-apps' }],
        groups: [
          { name: 'drgroup-2', namespace: 'erp-apps', consistencyLevel: 'vm' as const, vmNames: ['erp-app-1'] },
        ],
      },
    ],
    replicationHealth: [
      { name: 'drgroup-1', namespace: 'erp-db', health: 'Healthy', lastChecked: '2026-04-25T15:00:00Z' },
      { name: 'drgroup-2', namespace: 'erp-apps', health: 'Healthy', lastChecked: '2026-04-25T15:00:00Z' },
    ],
  },
};

describe('Keyboard accessibility — Dashboard', () => {
  it('plan row link is reachable via Tab and has correct href', async () => {
    const user = userEvent.setup();
    render(<DRDashboard />);
    const link = screen.getByRole('link', { name: 'erp-full-stack' });

    await user.tab();
    while (document.activeElement !== link && document.activeElement !== document.body) {
      await user.tab();
    }
    expect(link).toHaveFocus();
    expect(link).toHaveAttribute('href', '/disaster-recovery/plans/erp-full-stack');
  });
});

describe('Keyboard accessibility — DRLifecycleDiagram', () => {
  it('Failover button is reachable via Tab from SteadyState', async () => {
    const user = userEvent.setup();
    render(
      <DRLifecycleDiagram plan={mockSteadyStatePlan} onAction={jest.fn()} />,
    );
    const failoverButton = screen.getByRole('button', { name: 'Failover' });

    await user.tab();
    while (document.activeElement !== failoverButton && document.activeElement !== document.body) {
      await user.tab();
    }
    expect(failoverButton).toHaveFocus();
  });

  it('Enter on Failover button triggers onAction', async () => {
    const user = userEvent.setup();
    const onAction = jest.fn();
    render(
      <DRLifecycleDiagram plan={mockSteadyStatePlan} onAction={onAction} />,
    );
    const failoverButton = screen.getByRole('button', { name: 'Failover' });
    failoverButton.focus();
    await user.keyboard('{Enter}');
    expect(onAction).toHaveBeenCalledWith('failover', mockSteadyStatePlan);
  });

  it('Failback button is reachable via Tab from DRedSteadyState', async () => {
    const user = userEvent.setup();
    render(
      <DRLifecycleDiagram plan={mockDRedSteadyStatePlan} onAction={jest.fn()} />,
    );
    const failbackButton = screen.getByRole('button', { name: 'Failback' });

    await user.tab();
    while (document.activeElement !== failbackButton && document.activeElement !== document.body) {
      await user.tab();
    }
    expect(failbackButton).toHaveFocus();
  });

  it('no action buttons during transient phase', () => {
    render(
      <DRLifecycleDiagram plan={mockFailingOverPlan} onAction={jest.fn()} />,
    );
    expect(
      screen.queryByRole('button', { name: /failover|reprotect|failback|restore/i }),
    ).not.toBeInTheDocument();
  });
});

describe('Keyboard accessibility — WaveCompositionTree', () => {
  it('tree items are present and toggleable', async () => {
    const user = userEvent.setup();
    render(<WaveCompositionTree plan={mockPlanWithWaves} />);
    const treeItems = screen.getAllByRole('treeitem');
    expect(treeItems.length).toBeGreaterThanOrEqual(2);

    const wave1Toggle = treeItems[0].querySelector('button');
    expect(wave1Toggle).toBeTruthy();
    await user.click(wave1Toggle!);
    expect(screen.getByText('erp-db-1')).toBeInTheDocument();
  });
});
