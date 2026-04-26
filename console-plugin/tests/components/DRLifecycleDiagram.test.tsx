import { render, screen, fireEvent } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import DRLifecycleDiagram from '../../src/components/DRPlanDetail/DRLifecycleDiagram';
import { DRPlan } from '../../src/models/types';

expect.extend(toHaveNoViolations);

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
      discoveredVMCount: 12,
      conditions: [],
      ...overrides,
    },
  };
}

describe('DRLifecycleDiagram', () => {
  const mockOnAction = jest.fn();

  beforeEach(() => {
    mockOnAction.mockClear();
  });

  it('renders 4 phase nodes with correct labels', () => {
    const plan = makePlan();
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    expect(screen.getByText('Steady State')).toBeInTheDocument();
    expect(screen.getByText('Failed Over')).toBeInTheDocument();
    expect(screen.getByText('DR-ed Steady State')).toBeInTheDocument();
    expect(screen.getByText('Failed Back')).toBeInTheDocument();
  });

  it('highlights current phase (SteadyState) with full opacity', () => {
    const plan = makePlan({ phase: 'SteadyState' });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    const activeNode = screen.getByTestId('phase-node-SteadyState');
    expect(activeNode).toHaveStyle({ opacity: 1 });
  });

  it('fades non-current phases to 35% opacity', () => {
    const plan = makePlan({ phase: 'SteadyState' });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    const fadedNode = screen.getByTestId('phase-node-FailedOver');
    expect(fadedNode).toHaveStyle({ opacity: 0.35 });
  });

  it('renders Failover button with danger variant from SteadyState', () => {
    const plan = makePlan({ phase: 'SteadyState' });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    const btn = screen.getByRole('button', { name: 'Failover' });
    expect(btn).toBeInTheDocument();
    expect(btn).toHaveClass('pf-m-danger');
  });

  it('renders Reprotect button with secondary variant from FailedOver', () => {
    const plan = makePlan({ phase: 'FailedOver' });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    const btn = screen.getByRole('button', { name: 'Reprotect' });
    expect(btn).toBeInTheDocument();
    expect(btn).toHaveClass('pf-m-secondary');
  });

  it('renders Failback button with secondary variant from DRedSteadyState', () => {
    const plan = makePlan({ phase: 'DRedSteadyState' });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    const btn = screen.getByRole('button', { name: 'Failback' });
    expect(btn).toBeInTheDocument();
    expect(btn).toHaveClass('pf-m-secondary');
  });

  it('renders Restore button with secondary variant from FailedBack', () => {
    const plan = makePlan({ phase: 'FailedBack' });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    const btn = screen.getByRole('button', { name: 'Restore' });
    expect(btn).toBeInTheDocument();
    expect(btn).toHaveClass('pf-m-secondary');
  });

  it('shows no action buttons during transient phase (FailingOver)', () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    expect(screen.queryByRole('button', { name: 'Failover' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Reprotect' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Failback' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Restore' })).toBeNull();
  });

  it('shows "In progress..." during transient phase', () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    expect(screen.getByText('In progress...')).toBeInTheDocument();
  });

  it('gives destination node dashed border during transition', () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    const destNode = screen.getByTestId('phase-node-FailedOver');
    expect(destNode.getAttribute('style')).toMatch(/dashed/);
  });

  it('destination node has full opacity during transition', () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    const destNode = screen.getByTestId('phase-node-FailedOver');
    expect(destNode).toHaveStyle({ opacity: 1 });
  });

  it('calls onAction with correct args when Failover button is clicked', () => {
    const plan = makePlan({ phase: 'SteadyState' });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    fireEvent.click(screen.getByRole('button', { name: 'Failover' }));
    expect(mockOnAction).toHaveBeenCalledWith('Failover', plan);
  });

  it('calls onAction with correct args when Reprotect button is clicked', () => {
    const plan = makePlan({ phase: 'FailedOver' });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    fireEvent.click(screen.getByRole('button', { name: 'Reprotect' }));
    expect(mockOnAction).toHaveBeenCalledWith('Reprotect', plan);
  });

  it('calls onAction with correct args when Failback button is clicked', () => {
    const plan = makePlan({ phase: 'DRedSteadyState' });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    fireEvent.click(screen.getByRole('button', { name: 'Failback' }));
    expect(mockOnAction).toHaveBeenCalledWith('Failback', plan);
  });

  it('calls onAction with correct args when Restore button is clicked', () => {
    const plan = makePlan({ phase: 'FailedBack' });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    fireEvent.click(screen.getByRole('button', { name: 'Restore' }));
    expect(mockOnAction).toHaveBeenCalledWith('Restore', plan);
  });

  it('renders diagram container with role="figure" and aria-label', () => {
    const plan = makePlan();
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    const diagram = screen.getByRole('figure', { name: 'DR lifecycle state machine diagram' });
    expect(diagram).toBeInTheDocument();
  });

  it('renders phase nodes with role="group" and descriptive aria-label', () => {
    const plan = makePlan({ phase: 'SteadyState' });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    const node = screen.getByTestId('phase-node-SteadyState');
    expect(node).toHaveAttribute('role', 'group');
    expect(node.getAttribute('aria-label')).toContain('Steady State');
    expect(node.getAttribute('aria-label')).toContain('current phase');
  });

  it('announces transition via ARIA live region without wave progress', () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    const liveRegion = screen.getByRole('status');
    expect(liveRegion).toHaveAttribute('aria-live', 'polite');
    expect(liveRegion).toHaveTextContent('Failover in progress');
  });

  it('announces transition with wave progress when provided', () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} waveProgress={{ current: 2, total: 3 }} />);
    const liveRegion = screen.getByRole('status');
    expect(liveRegion).toHaveTextContent('Failover in progress, wave 2 of 3');
  });

  it('ARIA live region is empty during rest state', () => {
    const plan = makePlan({ phase: 'SteadyState' });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    const liveRegion = screen.getByRole('status');
    expect(liveRegion).toHaveTextContent('');
  });

  it('has no accessibility violations in rest state (SteadyState)', async () => {
    const plan = makePlan({ phase: 'SteadyState' });
    const { container } = render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations in transient state (FailingOver)', async () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-001',
      activeExecutionMode: 'disaster',
    });
    const { container } = render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('shows phase details: description, VM location, DC roles, replication', () => {
    const plan = makePlan({ phase: 'SteadyState' });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    expect(screen.getByText('Normal operations')).toBeInTheDocument();
    const steadyNode = screen.getByTestId('phase-node-SteadyState');
    expect(steadyNode).toHaveTextContent('VMs on DC1');
    expect(steadyNode).toHaveTextContent('DC1: Active (source)');
    expect(steadyNode).toHaveTextContent('DC2: Passive (target)');
    expect(steadyNode).toHaveTextContent('Replication: DC1 → DC2');
  });

  it('renders FailedOver phase details', () => {
    const plan = makePlan({ phase: 'FailedOver' });
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    expect(screen.getByText('Running on DR site')).toBeInTheDocument();
    const failedOverNode = screen.getByTestId('phase-node-FailedOver');
    expect(failedOverNode).toHaveTextContent('Replication: None');
  });

  it('defaults to SteadyState when status.phase is undefined', () => {
    const plan = makePlan({});
    delete (plan.status as Record<string, unknown>).phase;
    render(<DRLifecycleDiagram plan={plan} onAction={mockOnAction} />);
    const node = screen.getByTestId('phase-node-SteadyState');
    expect(node).toHaveStyle({ opacity: 1 });
  });
});
