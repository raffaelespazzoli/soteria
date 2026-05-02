import { render, screen, fireEvent } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import { SiteDisagreementAlert } from '../../src/components/DRPlanDetail/SiteDisagreementAlert';
import { DRPlan } from '../../src/models/types';

expect.extend(toHaveNoViolations);

function makePlan(sitesInSyncStatus?: 'True' | 'False', reason?: string, message?: string): DRPlan {
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
      conditions: sitesInSyncStatus
        ? [{ type: 'SitesInSync', status: sitesInSyncStatus, reason, message }]
        : [],
    },
  };
}

describe('SiteDisagreementAlert', () => {
  const mockOnSwitchToConfig = jest.fn();

  beforeEach(() => mockOnSwitchToConfig.mockClear());

  it('renders danger alert when SitesInSync=False', () => {
    const plan = makePlan(
      'False',
      'VMsMismatch',
      'VMs on primary but not secondary: [ns/vm-a, ns/vm-b]; VMs on secondary but not primary: [ns/vm-c]',
    );
    render(<SiteDisagreementAlert plan={plan} onSwitchToConfig={mockOnSwitchToConfig} />);

    expect(
      screen.getByText('Sites do not agree on VM inventory — DR operations are blocked'),
    ).toBeInTheDocument();
    expect(screen.getByText(/2 VMs on primary not found on secondary/)).toBeInTheDocument();
    expect(screen.getByText(/1 VM on secondary not found on primary/)).toBeInTheDocument();
  });

  it('renders nothing when SitesInSync=True', () => {
    const plan = makePlan('True', 'VMsAgreed');
    const { container } = render(
      <SiteDisagreementAlert plan={plan} onSwitchToConfig={mockOnSwitchToConfig} />,
    );
    expect(container.innerHTML).toBe('');
  });

  it('renders nothing when no SitesInSync condition exists (backward compat)', () => {
    const plan = makePlan();
    const { container } = render(
      <SiteDisagreementAlert plan={plan} onSwitchToConfig={mockOnSwitchToConfig} />,
    );
    expect(container.innerHTML).toBe('');
  });

  it('calls onSwitchToConfig when AlertActionLink is clicked', () => {
    const plan = makePlan(
      'False',
      'VMsMismatch',
      'VMs on primary but not secondary: [ns/vm-a]',
    );
    render(<SiteDisagreementAlert plan={plan} onSwitchToConfig={mockOnSwitchToConfig} />);
    fireEvent.click(screen.getByText('View site differences'));
    expect(mockOnSwitchToConfig).toHaveBeenCalledTimes(1);
  });

  it('disappears on rerender with SitesInSync=True', () => {
    const planFalse = makePlan('False', 'VMsMismatch', 'VMs on primary but not secondary: [ns/vm-a]');
    const { container, rerender } = render(
      <SiteDisagreementAlert plan={planFalse} onSwitchToConfig={mockOnSwitchToConfig} />,
    );
    expect(screen.getByText(/DR operations are blocked/)).toBeInTheDocument();

    const planTrue = makePlan('True', 'VMsAgreed');
    rerender(<SiteDisagreementAlert plan={planTrue} onSwitchToConfig={mockOnSwitchToConfig} />);
    expect(container.innerHTML).toBe('');
  });

  it('has no accessibility violations when alert is visible', async () => {
    const plan = makePlan(
      'False',
      'VMsMismatch',
      'VMs on primary but not secondary: [ns/vm-a]',
    );
    const { container } = render(
      <SiteDisagreementAlert plan={plan} onSwitchToConfig={mockOnSwitchToConfig} />,
    );
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations when alert is absent', async () => {
    const plan = makePlan('True', 'VMsAgreed');
    const { container } = render(
      <SiteDisagreementAlert plan={plan} onSwitchToConfig={mockOnSwitchToConfig} />,
    );
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
