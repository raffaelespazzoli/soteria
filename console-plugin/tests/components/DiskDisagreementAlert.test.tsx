import { render, screen, fireEvent } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import { DiskDisagreementAlert } from '../../src/components/DRPlanDetail/DiskDisagreementAlert';
import { DRPlan } from '../../src/models/types';

expect.extend(toHaveNoViolations);

function makePlan(
  disksConsistentStatus?: 'True' | 'False',
  reason?: string,
  message?: string,
): DRPlan {
  return {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRPlan',
    metadata: { name: 'test-plan', uid: '1', creationTimestamp: '' },
    spec: {
      maxConcurrentFailovers: 1,
      primarySite: 'dc-west',
      secondarySite: 'dc-east',
    },
    status: {
      phase: 'SteadyState',
      conditions: disksConsistentStatus
        ? [{ type: 'DisksConsistent', status: disksConsistentStatus, reason, message }]
        : [],
    },
  };
}

describe('DiskDisagreementAlert', () => {
  const mockOnSwitchToConfig = jest.fn();

  beforeEach(() => mockOnSwitchToConfig.mockClear());

  it('renders null when DisksConsistent condition is absent (backward compat)', () => {
    const plan = makePlan();
    const { container } = render(
      <DiskDisagreementAlert plan={plan} onSwitchToConfig={mockOnSwitchToConfig} />,
    );
    expect(container.innerHTML).toBe('');
  });

  it('renders null when DisksConsistent=True', () => {
    const plan = makePlan('True', 'DisksAgreed');
    const { container } = render(
      <DiskDisagreementAlert plan={plan} onSwitchToConfig={mockOnSwitchToConfig} />,
    );
    expect(container.innerHTML).toBe('');
  });

  it('renders danger alert with DiskMismatch title', () => {
    const plan = makePlan(
      'False',
      'DiskMismatch',
      'VM ns/vm-a: disk count differs (3 vs 2)',
    );
    render(<DiskDisagreementAlert plan={plan} onSwitchToConfig={mockOnSwitchToConfig} />);

    expect(
      screen.getByText('Disk topology does not match across sites — DR operations are blocked'),
    ).toBeInTheDocument();
    expect(screen.getByText('VM ns/vm-a: disk count differs (3 vs 2)')).toBeInTheDocument();
  });

  it('renders danger alert with StorageClassMixed title', () => {
    const plan = makePlan(
      'False',
      'StorageClassMixed',
      'Volume group vg-1 has mixed storage classes: [sc-a, sc-b]',
    );
    render(<DiskDisagreementAlert plan={plan} onSwitchToConfig={mockOnSwitchToConfig} />);

    expect(
      screen.getByText('Volume group storage classes are mixed — DR operations are blocked'),
    ).toBeInTheDocument();
    expect(
      screen.getByText('Volume group vg-1 has mixed storage classes: [sc-a, sc-b]'),
    ).toBeInTheDocument();
  });

  it('renders info alert for WaitingForDiskDiscovery (not danger)', () => {
    const plan = makePlan(
      'False',
      'WaitingForDiskDiscovery',
      'Disk discovery pending for secondary site',
    );
    render(<DiskDisagreementAlert plan={plan} onSwitchToConfig={mockOnSwitchToConfig} />);

    expect(
      screen.getByText('Waiting for disk discovery from both sites'),
    ).toBeInTheDocument();
    const alert = screen.getByText('Waiting for disk discovery from both sites').closest('.pf-v5-c-alert, [class*="pf-v6-c-alert"]');
    expect(alert).toBeTruthy();
  });

  it('action link calls onSwitchToConfig', () => {
    const plan = makePlan(
      'False',
      'DiskMismatch',
      'VM ns/vm-a: disk count differs',
    );
    render(<DiskDisagreementAlert plan={plan} onSwitchToConfig={mockOnSwitchToConfig} />);
    fireEvent.click(screen.getByText('View disk details'));
    expect(mockOnSwitchToConfig).toHaveBeenCalledTimes(1);
  });

  it('has no accessibility violations when alert is visible (DiskMismatch)', async () => {
    const plan = makePlan(
      'False',
      'DiskMismatch',
      'VM ns/vm-a: disk count differs',
    );
    const { container } = render(
      <DiskDisagreementAlert plan={plan} onSwitchToConfig={mockOnSwitchToConfig} />,
    );
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations when alert is visible (StorageClassMixed)', async () => {
    const plan = makePlan(
      'False',
      'StorageClassMixed',
      'Volume group vg-1 has mixed SCs',
    );
    const { container } = render(
      <DiskDisagreementAlert plan={plan} onSwitchToConfig={mockOnSwitchToConfig} />,
    );
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations when alert is absent', async () => {
    const plan = makePlan('True', 'DisksAgreed');
    const { container } = render(
      <DiskDisagreementAlert plan={plan} onSwitchToConfig={mockOnSwitchToConfig} />,
    );
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
