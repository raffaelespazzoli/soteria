import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import ExecutionSummary from '../../src/components/ExecutionDetail/ExecutionSummary';
import { DRExecution } from '../../src/models/types';

expect.extend(toHaveNoViolations);

const now = Date.now();

const mockSucceeded: DRExecution = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRExecution',
  metadata: { name: 'test-1', uid: '1' },
  spec: { planName: 'erp-full-stack', mode: 'disaster' },
  status: {
    isActive: false,
    phase: 'Succeeded',
    result: 'Succeeded',
    startTime: new Date(now - 17 * 60 * 1000).toISOString(),
    completionTime: new Date(now).toISOString(),
    waves: [
      {
        waveIndex: 0,
        groups: [
          { name: 'g1', result: 'Completed', vmNames: ['vm1', 'vm2'] },
          { name: 'g2', result: 'Completed', vmNames: ['vm3'] },
        ],
      },
      {
        waveIndex: 1,
        groups: [
          { name: 'g3', result: 'Completed', vmNames: ['vm4', 'vm5', 'vm6'] },
        ],
      },
    ],
  },
};

const mockPartial: DRExecution = {
  ...mockSucceeded,
  status: {
    ...mockSucceeded.status!,
    phase: 'PartiallySucceeded',
    result: 'PartiallySucceeded',
    waves: [
      {
        waveIndex: 0,
        groups: [
          { name: 'g1', result: 'Completed', vmNames: ['vm1', 'vm2'] },
          { name: 'g2', result: 'Failed', vmNames: ['vm3'], error: 'timeout' },
        ],
      },
    ],
  },
};

const mockActive: DRExecution = {
  ...mockSucceeded,
  status: {
    isActive: true,
    phase: 'Executing',
    startTime: new Date(now - 5 * 60 * 1000).toISOString(),
    waves: [
      {
        waveIndex: 0,
        groups: [
          { name: 'g1', result: 'InProgress', vmNames: ['vm1', 'vm2'] },
        ],
      },
    ],
  },
};

const mockReprotectSucceeded: DRExecution = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRExecution',
  metadata: { name: 'test-reprotect', uid: '4' },
  spec: { planName: 'erp-full-stack', mode: 'reprotect' },
  status: {
    isActive: false,
    phase: 'Succeeded',
    result: 'Succeeded',
    startTime: new Date(now - 41 * 1000).toISOString(),
    completionTime: new Date(now).toISOString(),
    conditions: [
      {
        type: 'ReprotectPhase',
        status: 'True',
        reason: 'Complete',
        message: 'Role setup: 6/6, healthy: 6/6',
        lastTransitionTime: new Date(now).toISOString(),
      },
    ],
  },
};

const mockReprotectFailed: DRExecution = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRExecution',
  metadata: { name: 'test-reprotect-fail', uid: '5' },
  spec: { planName: 'erp-full-stack', mode: 'reprotect' },
  status: {
    isActive: false,
    phase: 'Failed',
    result: 'Failed',
    startTime: new Date(now - 120 * 1000).toISOString(),
    completionTime: new Date(now).toISOString(),
    conditions: [
      {
        type: 'ReprotectPhase',
        status: 'False',
        reason: 'RoleSetupFailed',
        message: 'Role setup: 4/6, healthy: 4/6',
        lastTransitionTime: new Date(now).toISOString(),
      },
    ],
  },
};

describe('ExecutionSummary', () => {
  it('renders VM count and duration for succeeded execution', () => {
    render(<ExecutionSummary execution={mockSucceeded} />);
    expect(screen.getByText(/6 VMs recovered in/)).toBeInTheDocument();
  });

  it('renders result badge for Succeeded', () => {
    render(<ExecutionSummary execution={mockSucceeded} />);
    expect(screen.getByText('Succeeded')).toBeInTheDocument();
  });

  it('renders partial failure count', () => {
    render(<ExecutionSummary execution={mockPartial} />);
    expect(screen.getByText(/2 of 3 VMs recovered/)).toBeInTheDocument();
    expect(screen.getByText(/1 DRGroup failed/)).toBeInTheDocument();
  });

  it('does not render when execution is active (no completionTime)', () => {
    const { container } = render(<ExecutionSummary execution={mockActive} />);
    expect(container.firstChild).toBeNull();
  });

  it('renders with data-testid="execution-summary"', () => {
    render(<ExecutionSummary execution={mockSucceeded} />);
    expect(screen.getByTestId('execution-summary')).toBeInTheDocument();
  });

  it('renders with aria-label for the summary region', () => {
    render(<ExecutionSummary execution={mockSucceeded} />);
    expect(screen.getByRole('region', { name: 'Execution summary' })).toBeInTheDocument();
  });

  it('passes jest-axe (succeeded)', async () => {
    const { container } = render(<ExecutionSummary execution={mockSucceeded} />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it('passes jest-axe (partial)', async () => {
    const { container } = render(<ExecutionSummary execution={mockPartial} />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it('passes jest-axe (active — empty render)', async () => {
    const { container } = render(<ExecutionSummary execution={mockActive} />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it('renders reprotect summary for succeeded reprotect execution', () => {
    render(<ExecutionSummary execution={mockReprotectSucceeded} />);
    expect(screen.getByText(/Replication re-protected in/)).toBeInTheDocument();
    expect(screen.getByText(/Role setup: 6\/6, healthy: 6\/6/)).toBeInTheDocument();
  });

  it('renders failure message for failed reprotect execution', () => {
    render(<ExecutionSummary execution={mockReprotectFailed} />);
    expect(screen.getByText(/Re-protect failed/)).toBeInTheDocument();
    expect(screen.getByText(/Role setup: 4\/6, healthy: 4\/6/)).toBeInTheDocument();
  });

  it('passes jest-axe (reprotect succeeded)', async () => {
    const { container } = render(<ExecutionSummary execution={mockReprotectSucceeded} />);
    expect(await axe(container)).toHaveNoViolations();
  });
});
