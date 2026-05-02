import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import TransitionProgressBanner from '../../src/components/DRPlanDetail/TransitionProgressBanner';
import { DRExecution, DRPlan } from '../../src/models/types';
expect.extend(toHaveNoViolations);

jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
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

  it('renders "Failing Over in progress" during FailingOver', () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    render(<TransitionProgressBanner plan={plan} execution={makeExecution()} />);
    expect(screen.getByText('Failing Over in progress')).toBeInTheDocument();
  });

  it('renders "Reprotecting in progress" during Reprotecting', () => {
    const plan = makePlan({
      phase: 'FailedOver',
      activeExecution: 'exec-002',
      activeExecutionMode: 'reprotect',
    });
    render(<TransitionProgressBanner plan={plan} execution={makeExecution()} />);
    expect(screen.getByText('Reprotecting in progress')).toBeInTheDocument();
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

  it('renders execution details control during transition', () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    render(<TransitionProgressBanner plan={plan} execution={makeExecution()} />);
    expect(screen.getByRole('button', { name: 'View execution details' })).toBeInTheDocument();
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
    expect(screen.getByText('Failing Over in progress')).toBeInTheDocument();

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

  describe('optimistic execution state', () => {
    it('renders "Starting Failover..." with spinner when optimisticExec is set and execution is null', () => {
      const plan = makePlan({ phase: 'SteadyState' });
      render(
        <TransitionProgressBanner
          plan={plan}
          execution={null}
          optimisticExec={{ name: 'exec-opt', action: 'failover' }}
        />,
      );
      expect(screen.getByText('Starting Failover...')).toBeInTheDocument();
      expect(screen.getByLabelText('Execution starting')).toBeInTheDocument();
    });

    it('renders "Starting Planned Migration..." for planned_migration action', () => {
      const plan = makePlan({ phase: 'SteadyState' });
      render(
        <TransitionProgressBanner
          plan={plan}
          execution={null}
          optimisticExec={{ name: 'exec-opt', action: 'planned_migration' }}
        />,
      );
      expect(screen.getByText('Starting Planned Migration...')).toBeInTheDocument();
    });

    it('renders "Starting Reprotect..." for reprotect action', () => {
      const plan = makePlan({ phase: 'FailedOver' });
      render(
        <TransitionProgressBanner
          plan={plan}
          execution={null}
          optimisticExec={{ name: 'exec-opt', action: 'reprotect' }}
        />,
      );
      expect(screen.getByText('Starting Reprotect...')).toBeInTheDocument();
    });

    it('renders "Starting Failback..." for failback action', () => {
      const plan = makePlan({ phase: 'DRedSteadyState' });
      render(
        <TransitionProgressBanner
          plan={plan}
          execution={null}
          optimisticExec={{ name: 'exec-opt', action: 'failback' }}
        />,
      );
      expect(screen.getByText('Starting Failback...')).toBeInTheDocument();
    });

    it('renders "Starting Restore..." for restore action', () => {
      const plan = makePlan({ phase: 'FailedBack' });
      render(
        <TransitionProgressBanner
          plan={plan}
          execution={null}
          optimisticExec={{ name: 'exec-opt', action: 'restore' }}
        />,
      );
      expect(screen.getByText('Starting Restore...')).toBeInTheDocument();
    });

    it('renders real execution data when both optimisticExec and execution are provided', () => {
      const plan = makePlan({
        phase: 'SteadyState',
        activeExecution: 'exec-001',
        activeExecutionMode: 'disaster',
      });
      render(
        <TransitionProgressBanner
          plan={plan}
          execution={makeExecution()}
          optimisticExec={{ name: 'exec-opt', action: 'failover' }}
        />,
      );
      expect(screen.getByText('Failing Over in progress')).toBeInTheDocument();
      expect(screen.queryByText(/Starting Failover/)).not.toBeInTheDocument();
    });

    it('renders nothing when optimisticExec is null and plan is in rest state', () => {
      const plan = makePlan({ phase: 'SteadyState' });
      const { container } = render(
        <TransitionProgressBanner plan={plan} execution={null} optimisticExec={null} />,
      );
      expect(container.innerHTML).toBe('');
    });

    it('does not show "View execution details" link in optimistic state', () => {
      const plan = makePlan({ phase: 'SteadyState' });
      render(
        <TransitionProgressBanner
          plan={plan}
          execution={null}
          optimisticExec={{ name: 'exec-opt', action: 'failover' }}
        />,
      );
      expect(screen.queryByText('View execution details')).not.toBeInTheDocument();
    });

    it('banner stays mounted across transition from optimistic to real', () => {
      const plan = makePlan({ phase: 'SteadyState' });
      const { rerender } = render(
        <TransitionProgressBanner
          plan={plan}
          execution={null}
          optimisticExec={{ name: 'exec-opt', action: 'failover' }}
        />,
      );
      expect(screen.getByText('Starting Failover...')).toBeInTheDocument();
      expect(screen.getByTestId('transition-progress-banner')).toBeInTheDocument();

      const realPlan = makePlan({
        phase: 'SteadyState',
        activeExecution: 'exec-001',
        activeExecutionMode: 'disaster',
      });
      rerender(
        <TransitionProgressBanner
          plan={realPlan}
          execution={makeExecution()}
          optimisticExec={null}
        />,
      );
      expect(screen.getByText('Failing Over in progress')).toBeInTheDocument();
      expect(screen.getByTestId('transition-progress-banner')).toBeInTheDocument();
    });

    it('has no accessibility violations in optimistic state', async () => {
      const plan = makePlan({ phase: 'SteadyState' });
      const { container } = render(
        <TransitionProgressBanner
          plan={plan}
          execution={null}
          optimisticExec={{ name: 'exec-opt', action: 'failover' }}
        />,
      );
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });
  });
});
