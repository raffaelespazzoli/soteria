import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { axe, toHaveNoViolations } from 'jest-axe';
import ExecutionHeader from '../../src/components/ExecutionDetail/ExecutionHeader';
import { DRExecution, DRGroupResultValue } from '../../src/models/types';

expect.extend(toHaveNoViolations);

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({}));

const now = Date.now();

const baseExecution: DRExecution = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRExecution',
  metadata: { name: 'erp-failover-001', uid: '1' },
  spec: { planName: 'erp-full-stack', mode: 'disaster' },
  status: {
    startTime: new Date(now - 4 * 60 * 1000).toISOString(),
    waves: [
      {
        waveIndex: 0,
        startTime: new Date(now - 4 * 60 * 1000).toISOString(),
        completionTime: new Date(now - 2 * 60 * 1000).toISOString(),
        groups: [],
      },
      {
        waveIndex: 1,
        startTime: new Date(now - 2 * 60 * 1000).toISOString(),
        groups: [],
      },
    ],
  },
};

const completedExecution: DRExecution = {
  ...baseExecution,
  status: {
    startTime: new Date(now - 10 * 60 * 1000).toISOString(),
    completionTime: new Date(now).toISOString(),
    result: 'Succeeded',
    waves: [
      {
        waveIndex: 0,
        startTime: new Date(now - 10 * 60 * 1000).toISOString(),
        completionTime: new Date(now - 5 * 60 * 1000).toISOString(),
        groups: [],
      },
      {
        waveIndex: 1,
        startTime: new Date(now - 5 * 60 * 1000).toISOString(),
        completionTime: new Date(now).toISOString(),
        groups: [],
      },
    ],
  },
};

describe('ExecutionHeader', () => {
  it('renders execution name and mode label', () => {
    render(<ExecutionHeader execution={baseExecution} />);
    expect(screen.getByText('erp-failover-001')).toBeInTheDocument();
    expect(screen.getByText('Disaster Failover')).toBeInTheDocument();
  });

  it('shows elapsed time for active execution', () => {
    render(<ExecutionHeader execution={baseExecution} />);
    expect(screen.getByText(/Elapsed/)).toBeInTheDocument();
  });

  it('shows "calculating..." when no waves completed', () => {
    const noCompleted: DRExecution = {
      ...baseExecution,
      status: {
        startTime: new Date(now - 60000).toISOString(),
        waves: [{ waveIndex: 0, startTime: new Date(now).toISOString(), groups: [] }],
      },
    };
    render(<ExecutionHeader execution={noCompleted} />);
    expect(screen.getByText('calculating...')).toBeInTheDocument();
  });

  it('shows estimated remaining after first wave completes', () => {
    render(<ExecutionHeader execution={baseExecution} />);
    expect(screen.getByText(/Est\. remaining/)).toBeInTheDocument();
    expect(screen.getByText(/^~/)).toBeInTheDocument();
  });

  it('shows total duration and result badge when complete', () => {
    render(<ExecutionHeader execution={completedExecution} />);
    expect(screen.getByText(/Duration/)).toBeInTheDocument();
    expect(screen.getByText('Succeeded')).toBeInTheDocument();
  });

  it('applies monospace font to time displays', () => {
    const { container } = render(<ExecutionHeader execution={baseExecution} />);
    const monoElements = container.querySelectorAll('[style*="mono"]');
    expect(monoElements.length).toBeGreaterThan(0);
  });

  it('renders Planned Migration mode', () => {
    const planned: DRExecution = {
      ...baseExecution,
      spec: { ...baseExecution.spec, mode: 'planned_migration' },
    };
    render(<ExecutionHeader execution={planned} />);
    expect(screen.getByText('Planned Migration')).toBeInTheDocument();
  });

  it('renders Reprotect mode', () => {
    const reprotect: DRExecution = {
      ...baseExecution,
      spec: { ...baseExecution.spec, mode: 'reprotect' },
    };
    render(<ExecutionHeader execution={reprotect} />);
    expect(screen.getByText('Reprotect')).toBeInTheDocument();
  });

  it('has no accessibility violations (active)', async () => {
    const { container } = render(<ExecutionHeader execution={baseExecution} />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations (completed)', async () => {
    const { container } = render(<ExecutionHeader execution={completedExecution} />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});

const partialExecution: DRExecution = {
  ...baseExecution,
  status: {
    startTime: new Date(now - 10 * 60 * 1000).toISOString(),
    completionTime: new Date(now).toISOString(),
    result: 'PartiallySucceeded',
    waves: [
      {
        waveIndex: 0,
        startTime: new Date(now - 10 * 60 * 1000).toISOString(),
        completionTime: new Date(now - 5 * 60 * 1000).toISOString(),
        groups: [
          { name: 'drgroup-1', result: DRGroupResultValue.Completed, vmNames: ['db-1'] },
        ],
      },
      {
        waveIndex: 1,
        startTime: new Date(now - 5 * 60 * 1000).toISOString(),
        completionTime: new Date(now).toISOString(),
        groups: [
          { name: 'drgroup-3', result: DRGroupResultValue.Failed, vmNames: ['app-1'], error: 'timeout' },
          { name: 'drgroup-4', result: DRGroupResultValue.Failed, vmNames: ['app-2'], error: 'error 2' },
        ],
      },
    ],
  },
};

const singleFailExecution: DRExecution = {
  ...baseExecution,
  status: {
    ...partialExecution.status!,
    waves: [
      partialExecution.status!.waves![0],
      {
        waveIndex: 1,
        startTime: new Date(now - 5 * 60 * 1000).toISOString(),
        completionTime: new Date(now).toISOString(),
        groups: [
          { name: 'drgroup-3', result: DRGroupResultValue.Failed, vmNames: ['app-1'], error: 'timeout' },
          { name: 'drgroup-4', result: DRGroupResultValue.Completed, vmNames: ['app-2'] },
        ],
      },
    ],
  },
};

describe('ExecutionHeader — Retry All Failed', () => {
  it('shows "Retry All Failed" button when multiple groups failed and result is PartiallySucceeded', () => {
    render(<ExecutionHeader execution={partialExecution} onRetryAll={jest.fn()} />);
    expect(screen.getByRole('button', { name: /retry all failed/i })).toBeInTheDocument();
  });

  it('hides "Retry All Failed" button when only one group failed', () => {
    render(<ExecutionHeader execution={singleFailExecution} onRetryAll={jest.fn()} />);
    expect(screen.queryByRole('button', { name: /retry all failed/i })).not.toBeInTheDocument();
  });

  it('hides "Retry All Failed" when result is Succeeded', () => {
    render(<ExecutionHeader execution={completedExecution} onRetryAll={jest.fn()} />);
    expect(screen.queryByRole('button', { name: /retry all failed/i })).not.toBeInTheDocument();
  });

  it('calls onRetryAll when "Retry All Failed" is clicked', async () => {
    const user = userEvent.setup();
    const onRetryAll = jest.fn();
    render(<ExecutionHeader execution={partialExecution} onRetryAll={onRetryAll} />);
    await user.click(screen.getByRole('button', { name: /retry all failed/i }));
    expect(onRetryAll).toHaveBeenCalledTimes(1);
  });

  it('disables "Retry All Failed" when isRetryDisabled is true', () => {
    render(
      <ExecutionHeader
        execution={partialExecution}
        onRetryAll={jest.fn()}
        isRetryDisabled
        retryTooltip="Retry in progress"
      />,
    );
    expect(screen.getByRole('button', { name: /retry all failed/i })).toBeDisabled();
  });

  it('has no accessibility violations (with Retry All Failed)', async () => {
    const { container } = render(
      <ExecutionHeader execution={partialExecution} onRetryAll={jest.fn()} />,
    );
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations (disabled Retry All)', async () => {
    const { container } = render(
      <ExecutionHeader
        execution={partialExecution}
        onRetryAll={jest.fn()}
        isRetryDisabled
        retryTooltip="Retry in progress"
      />,
    );
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
