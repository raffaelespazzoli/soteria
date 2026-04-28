import { useState, useEffect } from 'react';
import { ProgressStep, ProgressStepProps, ExpandableSection, Spinner } from '@patternfly/react-core';
import {
  CheckCircleIcon,
  ExclamationCircleIcon,
  PendingIcon,
} from '@patternfly/react-icons';
import { WaveStatus, DRGroupExecutionStatus, DRGroupResultValue } from '../../models/types';
import { formatElapsedMs } from '../../hooks/useElapsedTime';

export type WaveState = 'pending' | 'inProgress' | 'completed' | 'partiallyFailed';

export function getWaveState(wave: WaveStatus): WaveState {
  if (wave.completionTime) {
    const hasFailed = wave.groups?.some((g) => g.result === DRGroupResultValue.Failed);
    return hasFailed ? 'partiallyFailed' : 'completed';
  }
  if (wave.startTime) return 'inProgress';
  return 'pending';
}

const WAVE_STATE_VARIANT: Record<WaveState, NonNullable<ProgressStepProps['variant']>> = {
  pending: 'pending',
  inProgress: 'info',
  completed: 'success',
  partiallyFailed: 'warning',
};

function getVMCount(wave: WaveStatus): number {
  if (!wave.groups) return 0;
  return wave.groups.reduce((sum, g) => sum + (g.vmNames?.length ?? 0), 0);
}

function getWaveElapsed(wave: WaveStatus): string {
  if (!wave.startTime) return '';
  const end = wave.completionTime ? new Date(wave.completionTime).getTime() : Date.now();
  return formatElapsedMs(end - new Date(wave.startTime).getTime());
}

function getGroupElapsed(group: DRGroupExecutionStatus): string {
  if (!group.startTime) return '';
  const end = group.completionTime ? new Date(group.completionTime).getTime() : Date.now();
  return formatElapsedMs(end - new Date(group.startTime).getTime());
}

interface GroupStatusDisplayProps {
  group: DRGroupExecutionStatus;
}

const iconStyle = (color: string): React.CSSProperties => ({
  color: `var(${color})`,
  marginRight: 'var(--pf-t--global--spacer--xs, var(--pf-v5-global--spacer--xs))',
});

const GroupStatusDisplay: React.FC<GroupStatusDisplayProps> = ({ group }) => {
  const result = group.result ?? DRGroupResultValue.Pending;

  switch (result) {
    case DRGroupResultValue.InProgress:
    case DRGroupResultValue.WaitingForVMReady:
      return (
        <span>
          <Spinner size="md" aria-label={`${group.name} in progress`} style={iconStyle('--pf-t--global--icon--color--status--info--default, --pf-v5-global--info-color--100')} />
          {' In Progress'}
        </span>
      );
    case DRGroupResultValue.Completed:
      return (
        <span>
          <CheckCircleIcon style={iconStyle('--pf-t--global--icon--color--status--success--default, --pf-v5-global--success-color--100')} />
          {' Completed'}
        </span>
      );
    case DRGroupResultValue.Failed:
      return (
        <span>
          <ExclamationCircleIcon style={iconStyle('--pf-t--global--icon--color--status--danger--default, --pf-v5-global--danger-color--100')} />
          {' Failed'}
          {group.error && (
            <span style={{ marginLeft: 'var(--pf-t--global--spacer--sm, var(--pf-v5-global--spacer--sm))', color: 'var(--pf-t--global--text--color--status--danger--default, var(--pf-v5-global--danger-color--100))' }}>
              — {group.error}
            </span>
          )}
        </span>
      );
    default:
      return (
        <span>
          <PendingIcon style={iconStyle('--pf-t--global--icon--color--disabled, --pf-v5-global--disabled-color--100')} />
          {' Pending'}
        </span>
      );
  }
};

interface WaveProgressStepProps {
  wave: WaveStatus;
  index: number;
}

const WaveProgressStep: React.FC<WaveProgressStepProps> = ({ wave, index }) => {
  const state = getWaveState(wave);
  const variant = WAVE_STATE_VARIANT[state];
  const isCurrent = state === 'inProgress';
  const vmCount = getVMCount(wave);
  const waveElapsed = getWaveElapsed(wave);

  const defaultExpanded = state === 'inProgress';
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);

  useEffect(() => {
    if (state === 'inProgress') setIsExpanded(true);
  }, [state]);

  const description =
    state === 'pending'
      ? `${vmCount} VMs`
      : `${vmCount} VMs${waveElapsed ? ` — ${waveElapsed}` : ''}`;

  return (
    <ProgressStep
      variant={variant}
      isCurrent={isCurrent}
      description={description}
      aria-label={`Wave ${index + 1}: ${state}`}
      id={`wave-${index}`}
      titleId={`wave-${index}-title`}
    >
      Wave {index + 1}
      {wave.groups && wave.groups.length > 0 && (
        <ExpandableSection
          toggleText={isExpanded ? 'Hide groups' : 'Show groups'}
          isExpanded={isExpanded}
          onToggle={(_e, expanded) => setIsExpanded(expanded)}
          style={{ marginTop: 'var(--pf-t--global--spacer--sm, var(--pf-v5-global--spacer--sm))' }}
        >
          <div
            role="list"
            aria-label={`Wave ${index + 1} groups`}
            style={{ paddingLeft: 'var(--pf-t--global--spacer--md, var(--pf-v5-global--spacer--md))' }}
          >
            {wave.groups.map((group, gIdx) => (
              <div
                key={`${group.name}-${gIdx}`}
                role="listitem"
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 'var(--pf-t--global--spacer--md, var(--pf-v5-global--spacer--md))',
                  padding: 'var(--pf-t--global--spacer--xs, var(--pf-v5-global--spacer--xs)) 0',
                  flexWrap: 'wrap',
                  fontSize: 'var(--pf-t--global--font--size--body--default, 14px)',
                }}
              >
                <strong>{group.name}</strong>
                <span style={{ color: 'var(--pf-t--global--text--color--subtle, var(--pf-v5-global--Color--200))' }}>
                  ({group.vmNames?.join(', ') ?? 'no VMs'})
                </span>
                <GroupStatusDisplay group={group} />
                {group.startTime && (
                  <span style={{ fontFamily: 'var(--pf-t--global--font--family--mono, var(--pf-v5-global--FontFamily--monospace))' }}>
                    {getGroupElapsed(group)}
                  </span>
                )}
              </div>
            ))}
          </div>
        </ExpandableSection>
      )}
    </ProgressStep>
  );
};

export default WaveProgressStep;
