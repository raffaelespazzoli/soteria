import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import userEvent from '@testing-library/user-event';
import { ProgressStepper } from '@patternfly/react-core';
import WaveProgressStep, { getWaveState } from '../../src/components/ExecutionDetail/WaveProgressStep';
import { WaveStatus, DRGroupResultValue } from '../../src/models/types';

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
    { name: 'drgroup-4', result: DRGroupResultValue.Failed, vmNames: ['app-2'], error: 'Storage sync timeout', startTime: new Date(Date.now() - 120000).toISOString() },
  ],
};

function renderInStepper(wave: WaveStatus, index: number) {
  return render(
    <ProgressStepper isVertical aria-label="test">
      <WaveProgressStep wave={wave} index={index} />
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

  it('shows error icon and error message for failed DRGroup', async () => {
    const user = userEvent.setup({ advanceTimers: jest.advanceTimersByTime });
    renderInStepper(partiallyFailedWave, 1);
    const toggle = screen.getByText('Show groups');
    await user.click(toggle);
    expect(screen.getByText(/Failed/)).toBeInTheDocument();
    expect(screen.getByText(/Storage sync timeout/)).toBeInTheDocument();
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
