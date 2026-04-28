import { useMemo } from 'react';
import {
  EmptyState,
  EmptyStateBody,
} from '@patternfly/react-core';
import { CubesIcon } from '@patternfly/react-icons';
import { Table, Thead, Tr, Th, Tbody, Td } from '@patternfly/react-table';
import { useHistory } from 'react-router-dom';
import ExecutionResultBadge from '../shared/ExecutionResultBadge';
import { DRExecution, DRExecutionResult } from '../../models/types';
import { formatDuration, formatRPO } from '../../utils/formatters';

function formatMode(mode: string | undefined): string {
  switch (mode) {
    case 'planned_migration':
      return 'Planned Migration';
    case 'disaster':
      return 'Disaster';
    case 'reprotect':
      return 'Re-protect';
    default:
      return mode ?? 'Unknown';
  }
}

function formatDate(dateStr: string | undefined): string {
  if (!dateStr) return 'N/A';
  const d = new Date(dateStr);
  return d.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function HistoryEmptyState() {
  return (
    <EmptyState variant="sm" titleText="No executions yet" icon={CubesIcon} headingLevel="h3">
      <EmptyStateBody>
        Trigger a planned migration to validate your DR plan
      </EmptyStateBody>
    </EmptyState>
  );
}

interface ExecutionHistoryTableProps {
  executions: DRExecution[];
  planName: string;
}

export const ExecutionHistoryTable: React.FC<ExecutionHistoryTableProps> = ({
  executions,
  planName,
}) => {
  const history = useHistory();
  const sorted = useMemo(() => {
    const filtered = executions.filter((e) => e.spec?.planName === planName);
    return [...filtered].sort(
      (a, b) =>
        new Date(b.status?.startTime ?? 0).getTime() -
        new Date(a.status?.startTime ?? 0).getTime(),
    );
  }, [executions, planName]);

  if (sorted.length === 0) return <HistoryEmptyState />;

  return (
    <Table aria-label="Execution history" variant="compact">
      <Thead>
        <Tr>
          <Th>Date</Th>
          <Th>Mode</Th>
          <Th>Result</Th>
          <Th>Duration</Th>
          <Th>RPO</Th>
          <Th>Triggered By</Th>
        </Tr>
      </Thead>
      <Tbody>
        {sorted.map((exec) => {
          const detailPath = `/disaster-recovery/executions/${exec.metadata?.name}`;
          return (
            <Tr
              key={exec.metadata?.name}
              isClickable
              onRowClick={() => history.push(detailPath)}
              style={{ cursor: 'pointer' }}
            >
              <Td dataLabel="Date">
                {formatDate(exec.status?.startTime)}
              </Td>
              <Td dataLabel="Mode">{formatMode(exec.spec?.mode)}</Td>
              <Td dataLabel="Result">
                {exec.status?.result ? (
                  <ExecutionResultBadge result={exec.status.result as DRExecutionResult} />
                ) : (
                  'In Progress'
                )}
              </Td>
              <Td dataLabel="Duration">
                {formatDuration(exec.status?.startTime, exec.status?.completionTime) || 'N/A'}
              </Td>
              <Td dataLabel="RPO">
                {exec.status?.rpoSeconds != null
                  ? formatRPO(exec.status.rpoSeconds) || 'N/A'
                  : 'N/A'}
              </Td>
              <Td dataLabel="Triggered By">
                {exec.metadata?.annotations?.['soteria.io/triggered-by'] ?? 'N/A'}
              </Td>
            </Tr>
          );
        })}
      </Tbody>
    </Table>
  );
};
