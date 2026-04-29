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
    result: 'Succeeded',
    startTime: new Date(now - 17 * 60 * 1000).toISOString(),
    completionTime: new Date(now).toISOString(),
    rpoSeconds: 47,
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

describe('ExecutionSummary', () => {
  it('renders VM count and duration for succeeded execution', () => {
    render(<ExecutionSummary execution={mockSucceeded} />);
    expect(screen.getByText(/6 VMs recovered in/)).toBeInTheDocument();
  });

  it('renders RPO in seconds', () => {
    render(<ExecutionSummary execution={mockSucceeded} />);
    expect(screen.getByText(/RPO: 47 seconds/)).toBeInTheDocument();
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

  it('does not render RPO when rpoSeconds is undefined', () => {
    const noRpo: DRExecution = {
      ...mockSucceeded,
      status: {
        ...mockSucceeded.status!,
        rpoSeconds: undefined,
      },
    };
    render(<ExecutionSummary execution={noRpo} />);
    expect(screen.queryByText(/RPO/)).not.toBeInTheDocument();
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
});
