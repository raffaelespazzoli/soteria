import { Label, LabelProps, Button, Tooltip } from '@patternfly/react-core';
import { DRExecution, DRExecutionMode, DRGroupResultValue, WaveStatus } from '../../models/types';
import { useElapsedTime, formatElapsedMs } from '../../hooks/useElapsedTime';
import { formatDuration, formatRPO } from '../../utils/formatters';
import ExecutionResultBadge from '../shared/ExecutionResultBadge';

const MODE_LABELS: Record<string, { label: string; status: NonNullable<LabelProps['status']> }> = {
  disaster: { label: 'Disaster Failover', status: 'danger' },
  planned_migration: { label: 'Planned Migration', status: 'info' },
  reprotect: { label: 'Reprotect', status: 'custom' },
};

function getModeDisplay(mode: DRExecutionMode) {
  return MODE_LABELS[mode] ?? { label: mode, status: 'custom' as const };
}

function estimateRemaining(
  elapsedMs: number,
  waves: WaveStatus[] | undefined,
): string {
  if (!waves || waves.length === 0) return 'calculating...';
  const completedWaves = waves.filter((w) => w.completionTime).length;
  if (completedWaves === 0) return 'calculating...';
  const avgPerWave = elapsedMs / completedWaves;
  const remainingMs = (waves.length - completedWaves) * avgPerWave;
  return `~${formatElapsedMs(remainingMs)}`;
}

const monoStyle: React.CSSProperties = {
  fontFamily: 'var(--pf-t--global--font--family--mono, var(--pf-v5-global--FontFamily--monospace))',
};

const lgFontStyle: React.CSSProperties = {
  fontSize: 'var(--pf-t--global--font--size--body--lg, var(--pf-v5-global--FontSize--lg))',
};

function getFailedGroupCount(execution: DRExecution): number {
  if (!execution.status?.waves) return 0;
  return execution.status.waves.reduce(
    (count, w) => count + (w.groups?.filter((g) => g.result === DRGroupResultValue.Failed).length ?? 0),
    0,
  );
}

interface ExecutionHeaderProps {
  execution: DRExecution;
  onRetryAll?: () => void;
  isRetryDisabled?: boolean;
  retryTooltip?: string;
}

const ExecutionHeader: React.FC<ExecutionHeaderProps> = ({
  execution,
  onRetryAll,
  isRetryDisabled = false,
  retryTooltip,
}) => {
  const { spec, status } = execution;
  const isComplete = !!status?.completionTime;
  const { elapsed, elapsedMs } = useElapsedTime(status?.startTime, !isComplete);
  const modeDisplay = getModeDisplay(spec.mode);
  const failedGroupCount = getFailedGroupCount(execution);
  const showRetryAll =
    status?.result === 'PartiallySucceeded' && failedGroupCount > 1;

  const headerStyle: React.CSSProperties = {
    marginBottom: 'var(--pf-t--global--spacer--md, var(--pf-v5-global--spacer--md))',
  };

  const nameStyle: React.CSSProperties = {
    fontSize: 'var(--pf-t--global--font--size--heading--h1, var(--pf-v5-global--FontSize--2xl))',
    fontWeight: 'var(--pf-t--global--font--weight--heading--default, 700)' as React.CSSProperties['fontWeight'],
    marginBottom: 'var(--pf-t--global--spacer--sm, var(--pf-v5-global--spacer--sm))',
  };

  const detailRowStyle: React.CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: 'var(--pf-t--global--spacer--md, var(--pf-v5-global--spacer--md))',
    flexWrap: 'wrap',
  };

  if (isComplete) {
    return (
      <div style={headerStyle} data-testid="execution-header">
        <div style={nameStyle}>{execution.metadata?.name}</div>
        <div style={detailRowStyle}>
          <Label status={modeDisplay.status} isCompact>
            {modeDisplay.label}
          </Label>
          <span>
            Duration:{' '}
            <span style={{ ...monoStyle, ...lgFontStyle }}>
              {formatDuration(status?.startTime, status?.completionTime)}
            </span>
          </span>
          {status?.result && <ExecutionResultBadge result={status.result} />}
          {status?.rpoSeconds != null && (
            <span style={lgFontStyle}>{formatRPO(status.rpoSeconds)}</span>
          )}
          {showRetryAll && (
            isRetryDisabled && retryTooltip ? (
              <Tooltip content={retryTooltip}>
                <Button variant="primary" size="sm" isDisabled aria-label="Retry All Failed">
                  Retry All Failed
                </Button>
              </Tooltip>
            ) : (
              <Button
                variant="primary"
                size="sm"
                onClick={onRetryAll}
                isDisabled={isRetryDisabled}
                aria-label="Retry All Failed"
              >
                Retry All Failed
              </Button>
            )
          )}
        </div>
      </div>
    );
  }

  return (
    <div style={headerStyle} data-testid="execution-header">
      <div style={nameStyle}>{execution.metadata?.name}</div>
      <div style={detailRowStyle}>
        <Label status={modeDisplay.status} isCompact>
          {modeDisplay.label}
        </Label>
        {status?.startTime && (
          <span>
            Started:{' '}
            <span style={{ ...monoStyle, ...lgFontStyle }}>
              {new Date(status.startTime).toLocaleTimeString([], {
                hour: '2-digit',
                minute: '2-digit',
              })}
            </span>
          </span>
        )}
        <span>
          Elapsed:{' '}
          <span style={{ ...monoStyle, ...lgFontStyle }}>{elapsed}</span>
        </span>
        <span>
          Est. remaining:{' '}
          <span style={{ ...monoStyle, ...lgFontStyle }}>
            {estimateRemaining(elapsedMs, status?.waves)}
          </span>
        </span>
      </div>
    </div>
  );
};

export default ExecutionHeader;
