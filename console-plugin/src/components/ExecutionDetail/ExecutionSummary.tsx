import { Card, CardBody } from '@patternfly/react-core';
import { DRExecution, DRGroupResultValue } from '../../models/types';
import { formatDuration } from '../../utils/formatters';
import ExecutionResultBadge from '../shared/ExecutionResultBadge';

function getVMCount(execution: DRExecution): number {
  return (execution.status?.waves ?? []).reduce(
    (total, wave) =>
      total +
      (wave.groups ?? []).reduce(
        (waveTotal, group) => waveTotal + (group.vmNames?.length ?? 0),
        0,
      ),
    0,
  );
}

function getFailedGroupCount(execution: DRExecution): number {
  return (execution.status?.waves ?? []).reduce(
    (total, wave) =>
      total +
      (wave.groups ?? []).filter((g) => g.result === DRGroupResultValue.Failed)
        .length,
    0,
  );
}

function getSucceededVMCount(execution: DRExecution): number {
  return (execution.status?.waves ?? []).reduce(
    (total, wave) =>
      total +
      (wave.groups ?? [])
        .filter((g) => g.result !== DRGroupResultValue.Failed)
        .reduce(
          (waveTotal, group) => waveTotal + (group.vmNames?.length ?? 0),
          0,
        ),
    0,
  );
}

const summaryStyle: React.CSSProperties = {
  fontSize:
    'var(--pf-t--global--font--size--heading--h3, var(--pf-v5-global--FontSize--xl))',
  lineHeight: 1.5,
};

interface ExecutionSummaryProps {
  execution: DRExecution;
}

const ExecutionSummary: React.FC<ExecutionSummaryProps> = ({ execution }) => {
  if (!execution.status?.completionTime) return null;

  const vmCount = getVMCount(execution);
  const failedCount = getFailedGroupCount(execution);
  const successCount = getSucceededVMCount(execution);
  const duration = formatDuration(
    execution.status.startTime,
    execution.status.completionTime,
  );
  const result = execution.status.result;

  return (
    <Card isCompact data-testid="execution-summary">
      <CardBody>
        <div style={summaryStyle} role="region" aria-label="Execution summary">
          <div>
            {result === 'Succeeded'
              ? `${vmCount} VMs recovered in ${duration}`
              : `${successCount} of ${vmCount} VMs recovered — ${failedCount} DRGroup failed`}
          </div>
          <div>
            Result: <ExecutionResultBadge result={result!} />
          </div>
        </div>
      </CardBody>
    </Card>
  );
};

export default ExecutionSummary;
