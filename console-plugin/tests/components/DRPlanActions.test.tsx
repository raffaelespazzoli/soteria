import { render, screen, fireEvent } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import DRPlanActions from '../../src/components/DRDashboard/DRPlanActions';
import { DRPlan } from '../../src/models/types';
import { EffectivePhase } from '../../src/utils/drPlanUtils';

expect.extend(toHaveNoViolations);

function makePlan(phase: string): DRPlan {
  return {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRPlan',
    metadata: { name: 'test-plan', uid: '1', creationTimestamp: '' },
    spec: { maxConcurrentFailovers: 1, primarySite: 'a', secondarySite: 'b', volumeReplicationDriver: { type: 'noop' } },
    status: { phase },
  };
}

describe('DRPlanActions', () => {
  const mockOnAction = jest.fn();

  beforeEach(() => mockOnAction.mockClear());

  it('renders kebab menu for SteadyState plan', () => {
    render(<DRPlanActions plan={makePlan('SteadyState')} effectivePhase="SteadyState" />);
    expect(screen.getByRole('button', { name: /actions for test-plan/i })).toBeInTheDocument();
  });

  it('shows Failover and Planned Migration for SteadyState', () => {
    render(<DRPlanActions plan={makePlan('SteadyState')} effectivePhase="SteadyState" />);
    fireEvent.click(screen.getByRole('button', { name: /actions for test-plan/i }));
    expect(screen.getByText('Failover')).toBeInTheDocument();
    expect(screen.getByText('Planned Migration')).toBeInTheDocument();
  });

  it('shows Reprotect for FailedOver', () => {
    render(<DRPlanActions plan={makePlan('FailedOver')} effectivePhase="FailedOver" />);
    fireEvent.click(screen.getByRole('button', { name: /actions for test-plan/i }));
    expect(screen.getByText('Reprotect')).toBeInTheDocument();
    expect(screen.queryByText('Failover')).not.toBeInTheDocument();
  });

  it('shows Failback for DRedSteadyState', () => {
    render(<DRPlanActions plan={makePlan('DRedSteadyState')} effectivePhase="DRedSteadyState" />);
    fireEvent.click(screen.getByRole('button', { name: /actions for test-plan/i }));
    expect(screen.getByText('Failback')).toBeInTheDocument();
  });

  it('shows Restore for FailedBack', () => {
    render(<DRPlanActions plan={makePlan('FailedBack')} effectivePhase="FailedBack" />);
    fireEvent.click(screen.getByRole('button', { name: /actions for test-plan/i }));
    expect(screen.getByText('Restore')).toBeInTheDocument();
  });

  it('renders nothing for transient phase (FailingOver)', () => {
    const { container } = render(
      <DRPlanActions plan={makePlan('SteadyState')} effectivePhase="FailingOver" />,
    );
    expect(container.innerHTML).toBe('');
  });

  it('renders nothing for transient phase (Reprotecting)', () => {
    const { container } = render(
      <DRPlanActions plan={makePlan('FailedOver')} effectivePhase="Reprotecting" />,
    );
    expect(container.innerHTML).toBe('');
  });

  it('calls onAction callback when menu item is clicked', () => {
    const plan = makePlan('SteadyState');
    render(<DRPlanActions plan={plan} effectivePhase="SteadyState" onAction={mockOnAction} />);
    fireEvent.click(screen.getByRole('button', { name: /actions for test-plan/i }));
    fireEvent.click(screen.getByText('Failover'));
    expect(mockOnAction).toHaveBeenCalledWith('failover', plan);
  });

  it('has no accessibility violations for SteadyState plan', async () => {
    const { container } = render(<DRPlanActions plan={makePlan('SteadyState')} effectivePhase="SteadyState" />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('renders disabled kebab with tooltip when isDisabled=true', () => {
    render(
      <DRPlanActions
        plan={makePlan('SteadyState')}
        effectivePhase="SteadyState"
        isDisabled={true}
        disabledTooltip="Plan blocked: sites do not agree on VM inventory"
      />,
    );
    const kebab = screen.getByRole('button', { name: /actions for test-plan/i });
    expect(kebab).toBeDisabled();
  });
});
