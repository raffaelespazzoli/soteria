import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import ExecutionHeader from '../../src/components/ExecutionDetail/ExecutionHeader';
import { DRExecution } from '../../src/models/types';

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
    rpoSeconds: 47,
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

  it('shows RPO when complete', () => {
    render(<ExecutionHeader execution={completedExecution} />);
    expect(screen.getByText('RPO 47s')).toBeInTheDocument();
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
