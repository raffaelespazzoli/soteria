import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import TransitionProgressBanner from '../../src/components/DRPlanDetail/TransitionProgressBanner';
import { DRExecution, DRPlan } from '../../src/models/types';

expect.extend(toHaveNoViolations);

jest.mock('react-router', () => ({
  ...jest.requireActual('react-router'),
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

function makePlan(overrides: Partial<DRPlan['status']> = {}): DRPlan {
  return {
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
      conditions: [],
      ...overrides,
    },
  };
}

function makeExecution(overrides: Partial<DRExecution['status']> = {}): DRExecution {
  return {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRExecution',
    metadata: { name: 'exec-001', uid: '2', creationTimestamp: '' },
    spec: { planName: 'erp-full-stack', mode: 'disaster' },
    status: {
      startTime: new Date(Date.now() - 60000).toISOString(),
      waves: [
        { waveIndex: 0, completionTime: new Date().toISOString() },
        { waveIndex: 1 },
        { waveIndex: 2 },
      ],
      ...overrides,
    },
  };
}

describe('TransitionProgressBanner', () => {
  it('renders nothing when plan is in rest state', () => {
    const plan = makePlan({ phase: 'SteadyState' });
    const { container } = render(
      <TransitionProgressBanner plan={plan} execution={null} />,
    );
    expect(container.innerHTML).toBe('');
  });

  it('renders "Failover in progress" during FailingOver', () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    render(<TransitionProgressBanner plan={plan} execution={makeExecution()} />);
    expect(screen.getByText('Failover in progress')).toBeInTheDocument();
  });

  it('renders "Reprotect in progress" during Reprotecting', () => {
    const plan = makePlan({
      phase: 'FailedOver',
      activeExecution: 'exec-002',
      activeExecutionMode: 'reprotect',
    });
    render(<TransitionProgressBanner plan={plan} execution={makeExecution()} />);
    expect(screen.getByText('Reprotect in progress')).toBeInTheDocument();
  });

  it('renders wave progress showing current in-progress wave', () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    render(<TransitionProgressBanner plan={plan} execution={makeExecution()} />);
    expect(screen.getByText(/Wave 2 of 3/)).toBeInTheDocument();
  });

  it('shows "Starting..." when no waves data', () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    const exec = makeExecution();
    delete exec.status!.waves;
    render(<TransitionProgressBanner plan={plan} execution={exec} />);
    expect(screen.getByText(/Starting\.\.\./)).toBeInTheDocument();
  });

  it('contains link to execution detail view', () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    render(<TransitionProgressBanner plan={plan} execution={makeExecution()} />);
    const link = screen.getByText('View execution details');
    expect(link.closest('a')).toHaveAttribute('href', '/disaster-recovery/executions/exec-001');
  });

  it('renders elapsed time', () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    render(<TransitionProgressBanner plan={plan} execution={makeExecution()} />);
    expect(screen.getByText(/Elapsed:/)).toBeInTheDocument();
  });

  it('banner disappears when transition completes (re-render with rest state)', () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    const { rerender, container } = render(
      <TransitionProgressBanner plan={plan} execution={makeExecution()} />,
    );
    expect(screen.getByText('Failover in progress')).toBeInTheDocument();

    const restPlan = makePlan({ phase: 'FailedOver' });
    rerender(<TransitionProgressBanner plan={restPlan} execution={null} />);
    expect(container.innerHTML).toBe('');
  });

  it('renders nothing when execution is null and plan is in rest state', () => {
    const plan = makePlan({ phase: 'FailedOver' });
    const { container } = render(
      <TransitionProgressBanner plan={plan} execution={null} />,
    );
    expect(container.innerHTML).toBe('');
  });

  it('has no accessibility violations during active transition', async () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    const { container } = render(
      <TransitionProgressBanner plan={plan} execution={makeExecution()} />,
    );
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations when banner is absent', async () => {
    const plan = makePlan({ phase: 'SteadyState' });
    const { container } = render(
      <TransitionProgressBanner plan={plan} execution={null} />,
    );
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
