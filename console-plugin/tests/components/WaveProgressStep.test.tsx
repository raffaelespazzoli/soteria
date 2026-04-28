import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import userEvent from '@testing-library/user-event';
import { ProgressStepper } from '@patternfly/react-core';
import WaveProgressStep, { getWaveState } from '../../src/components/ExecutionDetail/WaveProgressStep';
import { WaveStatus, DRGroupResultValue, DRExecutionResult } from '../../src/models/types';

expect.extend(toHaveNoViolations);

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({}));

const pendingWave: WaveStatus = {
  waveIndex: 2,
  groups: [
    { name: 'drgroup-5', result: DRGroupResultValue.Pending, vmNames: ['web-1', 'web-2'] },
  ],
};

const inProgressWave: WaveStatus = {
  waveIndex: 1,
  startTime: new Date(Date.now() - 60000).toISOString(),
  groups: [
    { name: 'drgroup-3', result: DRGroupResultValue.InProgress, vmNames: ['app-1', 'app-2'], startTime: new Date(Date.now() - 60000).toISOString() },
    { name: 'drgroup-4', result: DRGroupResultValue.Pending, vmNames: ['app-3'] },
  ],
};

const completedWave: WaveStatus = {
  waveIndex: 0,
  startTime: new Date(Date.now() - 120000).toISOString(),
  completionTime: new Date(Date.now() - 60000).toISOString(),
  groups: [
    { name: 'drgroup-1', result: DRGroupResultValue.Completed, vmNames: ['db-1', 'db-2'], startTime: new Date(Date.now() - 120000).toISOString(), completionTime: new Date(Date.now() - 60000).toISOString() },
  ],
};

const partiallyFailedWave: WaveStatus = {
  waveIndex: 1,
  startTime: new Date(Date.now() - 120000).toISOString(),
  completionTime: new Date(Date.now() - 30000).toISOString(),
  groups: [
    { name: 'drgroup-3', result: DRGroupResultValue.Completed, vmNames: ['app-1'], startTime: new Date(Date.now() - 120000).toISOString(), completionTime: new Date(Date.now() - 60000).toISOString() },
    {
      name: 'drgroup-4',
      result: DRGroupResultValue.Failed,
      vmNames: ['app-2'],
      error: 'Storage sync timeout',
      steps: [
        { name: 'StopReplication', status: 'Completed' },
        { name: 'StartVM', status: 'Failed', message: 'timeout' },
      ],
      startTime: new Date(Date.now() - 120000).toISOString(),
    },
  ],
};

const multiFailedWave: WaveStatus = {
  waveIndex: 1,
  startTime: new Date(Date.now() - 120000).toISOString(),
  completionTime: new Date(Date.now() - 30000).toISOString(),
  groups: [
    { name: 'drgroup-3', result: DRGroupResultValue.Failed, vmNames: ['app-1'], error: 'Error 1', startTime: new Date(Date.now() - 120000).toISOString() },
    { name: 'drgroup-4', result: DRGroupResultValue.Failed, vmNames: ['app-2'], error: 'Error 2', startTime: new Date(Date.now() - 120000).toISOString() },
  ],
};


interface RenderOptions {
  executionResult?: DRExecutionResult;
  onRetry?: (groupName: string) => void;
  isRetryDisabled?: boolean;
  retryTooltip?: string;
  retryError?: string | null;
  retriedGroup?: string | null;
}

function renderInStepper(wave: WaveStatus, index: number, opts?: RenderOptions) {
  return render(
    <ProgressStepper isVertical aria-label="test">
      <WaveProgressStep
        wave={wave}
        index={index}
        executionResult={opts?.executionResult}
        onRetry={opts?.onRetry}
        isRetryDisabled={opts?.isRetryDisabled}
        retryTooltip={opts?.retryTooltip}
        retryError={opts?.retryError}
        retriedGroup={opts?.retriedGroup}
      />
    </ProgressStepper>,
  );
}

describe('getWaveState', () => {
  it('returns pending for wave without start time', () => {
    expect(getWaveState(pendingWave)).toBe('pending');
  });

  it('returns inProgress for started but not completed wave', () => {
    expect(getWaveState(inProgressWave)).toBe('inProgress');
  });

  it('returns completed for completed wave with no failures', () => {
    expect(getWaveState(completedWave)).toBe('completed');
  });

  it('returns partiallyFailed for completed wave with a failed group', () => {
    expect(getWaveState(partiallyFailedWave)).toBe('partiallyFailed');
  });
});

describe('WaveProgressStep', () => {
  it('renders pending wave with pending variant', () => {
    renderInStepper(pendingWave, 2);
    expect(screen.getByText(/Wave 3/)).toBeInTheDocument();
    const step = screen.getByLabelText('Wave 3: pending');
    expect(step).toBeInTheDocument();
  });

  it('renders in-progress wave as current with auto-expanded groups', () => {
    renderInStepper(inProgressWave, 1);
    expect(screen.getByText(/Wave 2/)).toBeInTheDocument();
    expect(screen.getByLabelText('Wave 2: inProgress')).toBeInTheDocument();
    expect(screen.getByText('drgroup-3')).toBeInTheDocument();
    expect(screen.getByText(/app-1, app-2/)).toBeInTheDocument();
  });

  it('renders completed wave with success variant', () => {
    renderInStepper(completedWave, 0);
    expect(screen.getByLabelText('Wave 1: completed')).toBeInTheDocument();
    expect(screen.getByText(/Wave 1/)).toBeInTheDocument();
  });

  it('renders partially-failed wave with warning variant', () => {
    renderInStepper(partiallyFailedWave, 1);
    expect(screen.getByLabelText('Wave 2: partiallyFailed')).toBeInTheDocument();
  });

  it('shows DRGroup names and VM names in expanded view', () => {
    renderInStepper(inProgressWave, 1);
    expect(screen.getByText('drgroup-3')).toBeInTheDocument();
    expect(screen.getByText('drgroup-4')).toBeInTheDocument();
    expect(screen.getByText(/app-1, app-2/)).toBeInTheDocument();
    expect(screen.getByText(/app-3/)).toBeInTheDocument();
  });

  it('shows spinner for in-progress DRGroup', () => {
    renderInStepper(inProgressWave, 1);
    expect(screen.getByText('In Progress')).toBeInTheDocument();
  });

  it('shows error icon and error detail for failed DRGroup', () => {
    renderInStepper(partiallyFailedWave, 1);
    const failedLabels = screen.getAllByText(/Failed/);
    expect(failedLabels.length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText(/Storage sync timeout/)).toBeInTheDocument();
    expect(screen.getByText('StartVM')).toBeInTheDocument();
  });

  it('shows elapsed time for started DRGroups', () => {
    renderInStepper(inProgressWave, 1);
    const timeElements = screen.getAllByText(/\d+s|\d+m/);
    expect(timeElements.length).toBeGreaterThan(0);
  });

  it('can toggle expandable section', async () => {
    const user = userEvent.setup({ advanceTimers: jest.advanceTimersByTime });
    renderInStepper(inProgressWave, 1);
    expect(screen.getByText('drgroup-3')).toBeInTheDocument();
    const hideToggle = screen.getByText('Hide groups');
    await user.click(hideToggle);
    const showToggle = screen.getByText('Show groups');
    expect(showToggle).toBeInTheDocument();
  });

  it('has no accessibility violations', async () => {
    const { container } = renderInStepper(inProgressWave, 1);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});

describe('WaveProgressStep — retry', () => {
  it('shows Retry button for failed groups when result is PartiallySucceeded', () => {
    const onRetry = jest.fn();
    renderInStepper(partiallyFailedWave, 1, {
      executionResult: 'PartiallySucceeded',
      onRetry,
    });
    expect(screen.getByRole('button', { name: /retry drgroup-4/i })).toBeInTheDocument();
  });

  it('does not show Retry button during active execution (no result yet)', () => {
    renderInStepper(partiallyFailedWave, 1, { onRetry: jest.fn() });
    expect(screen.queryByRole('button', { name: /retry/i })).not.toBeInTheDocument();
  });

  it('does not show Retry button when result is Succeeded', () => {
    renderInStepper(partiallyFailedWave, 1, {
      executionResult: 'Succeeded',
      onRetry: jest.fn(),
    });
    expect(screen.queryByRole('button', { name: /retry/i })).not.toBeInTheDocument();
  });

  it('disables Retry button when isRetryDisabled is true', () => {
    renderInStepper(partiallyFailedWave, 1, {
      executionResult: 'PartiallySucceeded',
      onRetry: jest.fn(),
      isRetryDisabled: true,
      retryTooltip: 'Retry in progress — wait for current retry to complete',
    });
    expect(screen.getByRole('button', { name: /retry drgroup-4/i })).toBeDisabled();
  });

  it('calls onRetry with group name when Retry is clicked', async () => {
    const user = userEvent.setup();
    const onRetry = jest.fn();
    renderInStepper(partiallyFailedWave, 1, {
      executionResult: 'PartiallySucceeded',
      onRetry,
    });
    await user.click(screen.getByRole('button', { name: /retry drgroup-4/i }));
    expect(onRetry).toHaveBeenCalledWith('drgroup-4');
  });

  it('has correct aria-label on Retry button', () => {
    renderInStepper(partiallyFailedWave, 1, {
      executionResult: 'PartiallySucceeded',
      onRetry: jest.fn(),
    });
    const btn = screen.getByRole('button', { name: /retry drgroup-4/i });
    expect(btn).toHaveAttribute('aria-label', 'Retry drgroup-4');
  });

  it('has no accessibility violations for retry states', async () => {
    const { container } = renderInStepper(partiallyFailedWave, 1, {
      executionResult: 'PartiallySucceeded',
      onRetry: jest.fn(),
    });
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations when retry is disabled', async () => {
    const { container } = renderInStepper(partiallyFailedWave, 1, {
      executionResult: 'PartiallySucceeded',
      onRetry: jest.fn(),
      isRetryDisabled: true,
      retryTooltip: 'Retry in progress',
    });
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('scopes retryError to the retried group only', () => {
    renderInStepper(multiFailedWave, 1, {
      executionResult: 'PartiallySucceeded',
      onRetry: jest.fn(),
      retryError: 'VM unreachable',
      retriedGroup: 'drgroup-3',
    });
    const errorTexts = screen.getAllByText('VM unreachable');
    expect(errorTexts).toHaveLength(1);
  });

  it('shows retryError on all failed groups when retriedGroup is all-failed', () => {
    renderInStepper(multiFailedWave, 1, {
      executionResult: 'PartiallySucceeded',
      onRetry: jest.fn(),
      retryError: 'VM unreachable',
      retriedGroup: 'all-failed',
    });
    const errorTexts = screen.getAllByText('VM unreachable');
    expect(errorTexts).toHaveLength(2);
  });

  it('does not show retryError on unrelated group', () => {
    renderInStepper(multiFailedWave, 1, {
      executionResult: 'PartiallySucceeded',
      onRetry: jest.fn(),
      retryError: 'VM unreachable',
      retriedGroup: 'drgroup-99',
    });
    expect(screen.queryByText('VM unreachable')).not.toBeInTheDocument();
  });
});
