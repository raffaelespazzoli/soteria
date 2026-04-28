import { render, screen, fireEvent } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import DRPlanActions from '../../src/components/DRDashboard/DRPlanActions';
import { DRPlan } from '../../src/models/types';

expect.extend(toHaveNoViolations);

function makePlan(phase: string, activeExecution?: string, activeExecutionMode?: string): DRPlan {
  return {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRPlan',
    metadata: { name: 'test-plan', uid: '1', creationTimestamp: '' },
    spec: { waveLabel: 'w', maxConcurrentFailovers: 1, primarySite: 'a', secondarySite: 'b' },
    status: {
      phase,
      ...(activeExecution ? { activeExecution } : {}),
      ...(activeExecutionMode
        ? { activeExecutionMode: activeExecutionMode as 'planned_migration' | 'disaster' | 'reprotect' }
        : {}),
    },
  };
}

describe('DRPlanActions', () => {
  const mockOnAction = jest.fn();

  beforeEach(() => mockOnAction.mockClear());

  it('renders kebab menu for SteadyState plan', () => {
    render(<DRPlanActions plan={makePlan('SteadyState')} />);
    expect(screen.getByRole('button', { name: /actions for test-plan/i })).toBeInTheDocument();
  });

  it('shows Failover and Planned Migration for SteadyState', () => {
    render(<DRPlanActions plan={makePlan('SteadyState')} />);
    fireEvent.click(screen.getByRole('button', { name: /actions for test-plan/i }));
    expect(screen.getByText('Failover')).toBeInTheDocument();
    expect(screen.getByText('Planned Migration')).toBeInTheDocument();
  });

  it('shows Reprotect for FailedOver', () => {
    render(<DRPlanActions plan={makePlan('FailedOver')} />);
    fireEvent.click(screen.getByRole('button', { name: /actions for test-plan/i }));
    expect(screen.getByText('Reprotect')).toBeInTheDocument();
    expect(screen.queryByText('Failover')).not.toBeInTheDocument();
  });

  it('shows Failback for DRedSteadyState', () => {
    render(<DRPlanActions plan={makePlan('DRedSteadyState')} />);
    fireEvent.click(screen.getByRole('button', { name: /actions for test-plan/i }));
    expect(screen.getByText('Failback')).toBeInTheDocument();
  });

  it('shows Restore for FailedBack', () => {
    render(<DRPlanActions plan={makePlan('FailedBack')} />);
    fireEvent.click(screen.getByRole('button', { name: /actions for test-plan/i }));
    expect(screen.getByText('Restore')).toBeInTheDocument();
  });

  it('renders nothing for transient phase (FailingOver)', () => {
    const { container } = render(
      <DRPlanActions plan={makePlan('SteadyState', 'exec-1', 'planned_migration')} />,
    );
    expect(container.innerHTML).toBe('');
  });

  it('renders nothing for transient phase (Reprotecting)', () => {
    const { container } = render(
      <DRPlanActions plan={makePlan('FailedOver', 'exec-1', 'reprotect')} />,
    );
    expect(container.innerHTML).toBe('');
  });

  it('calls onAction callback when menu item is clicked', () => {
    const plan = makePlan('SteadyState');
    render(<DRPlanActions plan={plan} onAction={mockOnAction} />);
    fireEvent.click(screen.getByRole('button', { name: /actions for test-plan/i }));
    fireEvent.click(screen.getByText('Failover'));
    expect(mockOnAction).toHaveBeenCalledWith('failover', plan);
  });

  it('has no accessibility violations for SteadyState plan', async () => {
    const { container } = render(<DRPlanActions plan={makePlan('SteadyState')} />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
