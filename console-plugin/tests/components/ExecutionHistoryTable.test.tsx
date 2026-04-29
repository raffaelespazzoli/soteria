import { render, screen, fireEvent } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import { ExecutionHistoryTable } from '../../src/components/DRPlanDetail/ExecutionHistoryTable';
import { DRExecution } from '../../src/models/types';

expect.extend(toHaveNoViolations);

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  useK8sWatchResource: jest.fn(() => [null, false, null]),
}));

const mockPush = jest.fn();
jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useHistory: () => ({ push: mockPush }),
}));

const mockExecutions: DRExecution[] = [
  {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRExecution',
    metadata: {
      name: 'erp-full-stack-failover-001',
      uid: 'e1',
      annotations: { 'soteria.io/triggered-by': 'carlos@corp' },
    },
    spec: { planName: 'erp-full-stack', mode: 'disaster' },
    status: {
      result: 'PartiallySucceeded',
      startTime: '2026-03-18T03:14:00Z',
      completionTime: '2026-03-18T03:36:41Z',
    },
  },
  {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRExecution',
    metadata: {
      name: 'erp-full-stack-migration-002',
      uid: 'e2',
      annotations: { 'soteria.io/triggered-by': 'maya@corp' },
    },
    spec: { planName: 'erp-full-stack', mode: 'planned_migration' },
    status: {
      result: 'Succeeded',
      startTime: '2026-04-20T14:32:00Z',
      completionTime: '2026-04-20T14:49:22Z',
    },
  },
];

const otherPlanExecution: DRExecution = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRExecution',
  metadata: { name: 'other-plan-exec', uid: 'e3' },
  spec: { planName: 'other-plan', mode: 'disaster' },
  status: { result: 'Failed', startTime: '2026-04-01T00:00:00Z' },
};

describe('ExecutionHistoryTable', () => {
  it('renders table with correct columns', () => {
    render(<ExecutionHistoryTable executions={mockExecutions} planName="erp-full-stack" />);
    expect(screen.getByText('Date')).toBeInTheDocument();
    expect(screen.getByText('Mode')).toBeInTheDocument();
    expect(screen.getByText('Result')).toBeInTheDocument();
    expect(screen.getByText('Duration')).toBeInTheDocument();
    expect(screen.getByText('Triggered By')).toBeInTheDocument();
  });

  it('renders 2 rows of execution data', () => {
    render(<ExecutionHistoryTable executions={mockExecutions} planName="erp-full-stack" />);
    expect(screen.getByText('carlos@corp')).toBeInTheDocument();
    expect(screen.getByText('maya@corp')).toBeInTheDocument();
  });

  it('shows most recent execution first (date descending)', () => {
    render(<ExecutionHistoryTable executions={mockExecutions} planName="erp-full-stack" />);
    const rows = screen.getAllByRole('row');
    const dataRows = rows.slice(1);
    expect(dataRows.length).toBe(2);
    expect(dataRows[0]).toHaveTextContent('maya@corp');
    expect(dataRows[1]).toHaveTextContent('carlos@corp');
  });

  it('displays disaster mode as "Disaster"', () => {
    render(<ExecutionHistoryTable executions={mockExecutions} planName="erp-full-stack" />);
    expect(screen.getByText('Disaster')).toBeInTheDocument();
  });

  it('displays planned_migration mode as "Planned Migration"', () => {
    render(<ExecutionHistoryTable executions={mockExecutions} planName="erp-full-stack" />);
    expect(screen.getByText('Planned Migration')).toBeInTheDocument();
  });

  it('renders ExecutionResultBadge for result column', () => {
    render(<ExecutionHistoryTable executions={mockExecutions} planName="erp-full-stack" />);
    expect(screen.getByText('Partial')).toBeInTheDocument();
    expect(screen.getByText('Succeeded')).toBeInTheDocument();
  });

  it('navigates to execution detail when row is clicked', () => {
    mockPush.mockClear();
    render(<ExecutionHistoryTable executions={mockExecutions} planName="erp-full-stack" />);
    const rows = screen.getAllByRole('row');
    const dataRows = rows.slice(1);
    fireEvent.click(dataRows[0]);
    expect(mockPush).toHaveBeenCalledWith(
      '/disaster-recovery/executions/erp-full-stack-migration-002',
    );
    fireEvent.click(dataRows[1]);
    expect(mockPush).toHaveBeenCalledWith(
      '/disaster-recovery/executions/erp-full-stack-failover-001',
    );
  });

  it('renders empty state when no executions exist', () => {
    render(<ExecutionHistoryTable executions={[]} planName="erp-full-stack" />);
    expect(screen.getByText('No executions yet')).toBeInTheDocument();
    expect(
      screen.getByText('Trigger a planned migration to validate your DR plan'),
    ).toBeInTheDocument();
  });

  it('filters executions by planName', () => {
    const allExecutions = [...mockExecutions, otherPlanExecution];
    render(<ExecutionHistoryTable executions={allExecutions} planName="erp-full-stack" />);
    const rows = screen.getAllByRole('row');
    expect(rows.length).toBe(3); // header + 2 data rows
  });

  it('shows empty state when all executions belong to other plans', () => {
    render(<ExecutionHistoryTable executions={[otherPlanExecution]} planName="erp-full-stack" />);
    expect(screen.getByText('No executions yet')).toBeInTheDocument();
  });

  it('renders table with aria-label', () => {
    render(<ExecutionHistoryTable executions={mockExecutions} planName="erp-full-stack" />);
    expect(screen.getByLabelText('Execution history')).toBeInTheDocument();
  });

  it('has no accessibility violations with data', async () => {
    const { container } = render(
      <ExecutionHistoryTable executions={mockExecutions} planName="erp-full-stack" />,
    );
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('has no accessibility violations with empty state', async () => {
    const { container } = render(
      <ExecutionHistoryTable executions={[]} planName="erp-full-stack" />,
    );
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
