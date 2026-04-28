import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { axe, toHaveNoViolations } from 'jest-axe';
import FailedGroupDetail, { getFailedStep } from '../../src/components/ExecutionDetail/FailedGroupDetail';
import { DRGroupExecutionStatus } from '../../src/models/types';

expect.extend(toHaveNoViolations);

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({}));

const mockFailedGroup: DRGroupExecutionStatus = {
  name: 'drgroup-3',
  result: 'Failed',
  vmNames: ['erp-app-1', 'erp-app-2', 'erp-app-3'],
  error: 'storage driver timeout on SetSource for volume-group vg-erp-app',
  steps: [
    { name: 'StopReplication', status: 'Completed', timestamp: '2026-04-28T10:00:00Z' },
    { name: 'StartVM', status: 'Failed', message: 'timeout after 5m', timestamp: '2026-04-28T10:02:15Z' },
  ],
  retryCount: 1,
  startTime: '2026-04-28T10:00:00Z',
  completionTime: '2026-04-28T10:02:15Z',
};

describe('getFailedStep', () => {
  it('returns the step with Failed status', () => {
    expect(getFailedStep(mockFailedGroup)).toBe('StartVM');
  });

  it('returns null when no steps', () => {
    expect(getFailedStep({ ...mockFailedGroup, steps: undefined })).toBeNull();
    expect(getFailedStep({ ...mockFailedGroup, steps: [] })).toBeNull();
  });

  it('returns last non-Completed step when none explicitly Failed', () => {
    const group: DRGroupExecutionStatus = {
      ...mockFailedGroup,
      steps: [
        { name: 'StopReplication', status: 'Completed' },
        { name: 'StartVM', status: 'InProgress' },
      ],
    };
    expect(getFailedStep(group)).toBe('StartVM');
  });

  it('returns null when all steps are Completed', () => {
    const group: DRGroupExecutionStatus = {
      ...mockFailedGroup,
      steps: [
        { name: 'StopReplication', status: 'Completed' },
        { name: 'StartVM', status: 'Completed' },
      ],
    };
    expect(getFailedStep(group)).toBeNull();
  });
});

describe('FailedGroupDetail', () => {
  it('renders error message from group.error', () => {
    render(<FailedGroupDetail group={mockFailedGroup} showRetryButton={false} isRetryDisabled={false} />);
    expect(screen.getByText(/storage driver timeout/)).toBeInTheDocument();
  });

  it('shows affected VM names', () => {
    render(<FailedGroupDetail group={mockFailedGroup} showRetryButton={false} isRetryDisabled={false} />);
    expect(screen.getByText(/erp-app-1/)).toBeInTheDocument();
    expect(screen.getByText(/erp-app-2/)).toBeInTheDocument();
    expect(screen.getByText(/erp-app-3/)).toBeInTheDocument();
  });

  it('shows failed step name', () => {
    render(<FailedGroupDetail group={mockFailedGroup} showRetryButton={false} isRetryDisabled={false} />);
    expect(screen.getByText('StartVM')).toBeInTheDocument();
  });

  it('shows retry count when > 0', () => {
    render(<FailedGroupDetail group={mockFailedGroup} showRetryButton={false} isRetryDisabled={false} />);
    expect(screen.getByText(/previously retried 1 time/i)).toBeInTheDocument();
  });

  it('does not show retry count when 0', () => {
    render(<FailedGroupDetail group={{ ...mockFailedGroup, retryCount: 0 }} showRetryButton={false} isRetryDisabled={false} />);
    expect(screen.queryByText(/previously retried/i)).not.toBeInTheDocument();
  });

  it('does not show retry count when undefined', () => {
    render(<FailedGroupDetail group={{ ...mockFailedGroup, retryCount: undefined }} showRetryButton={false} isRetryDisabled={false} />);
    expect(screen.queryByText(/previously retried/i)).not.toBeInTheDocument();
  });

  it('uses plural "times" for retryCount > 1', () => {
    render(<FailedGroupDetail group={{ ...mockFailedGroup, retryCount: 3 }} showRetryButton={false} isRetryDisabled={false} />);
    expect(screen.getByText(/previously retried 3 times/i)).toBeInTheDocument();
  });

  it('renders ExpandableSection auto-expanded', () => {
    const { container } = render(
      <FailedGroupDetail group={mockFailedGroup} showRetryButton={false} isRetryDisabled={false} />,
    );
    const section = container.querySelector('[class*="expandable-section"]');
    expect(section).toBeInTheDocument();
  });

  it('renders retry button when showRetryButton is true', () => {
    render(
      <FailedGroupDetail group={mockFailedGroup} showRetryButton onRetry={jest.fn()} isRetryDisabled={false} />,
    );
    expect(screen.getByRole('button', { name: /retry drgroup-3/i })).toBeInTheDocument();
  });

  it('does not render retry button when showRetryButton is false', () => {
    render(
      <FailedGroupDetail group={mockFailedGroup} showRetryButton={false} isRetryDisabled={false} />,
    );
    expect(screen.queryByRole('button', { name: /retry/i })).not.toBeInTheDocument();
  });

  it('calls onRetry when retry button is clicked', async () => {
    const user = userEvent.setup();
    const onRetry = jest.fn();
    render(
      <FailedGroupDetail group={mockFailedGroup} showRetryButton onRetry={onRetry} isRetryDisabled={false} />,
    );
    await user.click(screen.getByRole('button', { name: /retry drgroup-3/i }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it('disables retry button when isRetryDisabled is true', () => {
    render(
      <FailedGroupDetail
        group={mockFailedGroup}
        showRetryButton
        onRetry={jest.fn()}
        isRetryDisabled
        retryTooltip="Retry in progress"
      />,
    );
    expect(screen.getByRole('button', { name: /retry drgroup-3/i })).toBeDisabled();
  });

  it('shows retry error when provided', () => {
    render(
      <FailedGroupDetail
        group={mockFailedGroup}
        showRetryButton
        isRetryDisabled={false}
        retryError="VM erp-app-1 is in an unpredictable state"
      />,
    );
    expect(screen.getByText(/unpredictable state/)).toBeInTheDocument();
  });

  it('does not show retry error when null', () => {
    render(
      <FailedGroupDetail
        group={mockFailedGroup}
        showRetryButton
        isRetryDisabled={false}
        retryError={null}
      />,
    );
    expect(screen.queryByText(/Retry rejected/)).not.toBeInTheDocument();
  });

  it('shows "Unknown error" when group.error is undefined', () => {
    render(
      <FailedGroupDetail
        group={{ ...mockFailedGroup, error: undefined }}
        showRetryButton={false}
        isRetryDisabled={false}
      />,
    );
    expect(screen.getByText('Unknown error')).toBeInTheDocument();
  });

  it('passes jest-axe (with retry button)', async () => {
    const { container } = render(
      <FailedGroupDetail group={mockFailedGroup} showRetryButton onRetry={jest.fn()} isRetryDisabled={false} />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it('passes jest-axe (without retry button)', async () => {
    const { container } = render(
      <FailedGroupDetail group={mockFailedGroup} showRetryButton={false} isRetryDisabled={false} />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it('passes jest-axe (with retry error)', async () => {
    const { container } = render(
      <FailedGroupDetail
        group={mockFailedGroup}
        showRetryButton
        isRetryDisabled={false}
        retryError="VM unreachable"
      />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it('passes jest-axe (disabled retry button)', async () => {
    const { container } = render(
      <FailedGroupDetail
        group={mockFailedGroup}
        showRetryButton
        isRetryDisabled
        retryTooltip="Retry in progress"
      />,
    );
    expect(await axe(container)).toHaveNoViolations();
  });
});
