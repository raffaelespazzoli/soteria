import { useEffect, useRef } from 'react';
import { useDRExecutions } from './useDRResources';
import { addToast } from '../notifications/toastStore';
import { DRExecution, DRExecutionResult, DRGroupResultValue } from '../models/types';
import { formatDuration } from '../utils/formatters';

const MODE_LABELS: Record<string, string> = {
  disaster: 'Failover',
  planned_migration: 'Planned Migration',
  reprotect: 'Re-protect',
};

function getModeLabel(mode: string): string {
  return MODE_LABELS[mode] ?? mode;
}

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

function getExecutionLink(execution: DRExecution): string {
  return `/disaster-recovery/executions/${execution.metadata?.name ?? ''}`;
}

function getPlanName(execution: DRExecution): string {
  return (
    (execution.metadata?.labels as Record<string, string> | undefined)?.[
      'soteria.io/plan-name'
    ] ??
    execution.spec?.planName ??
    ''
  );
}

function fireStartedToast(execution: DRExecution): void {
  const modeLabel = getModeLabel(execution.spec?.mode);
  const planName = getPlanName(execution);
  addToast({
    variant: 'info',
    title: `${modeLabel} started for ${planName}`,
    linkText: 'View execution',
    linkTo: getExecutionLink(execution),
    persistent: false,
    timeout: 8000,
  });
}

function fireCompletedToast(execution: DRExecution): void {
  const result = execution.status?.result as DRExecutionResult;
  const modeLabel = getModeLabel(execution.spec?.mode);
  const planName = getPlanName(execution);
  const link = getExecutionLink(execution);

  if (execution.spec?.mode === 'reprotect' && result === 'Succeeded') {
    addToast({
      variant: 'success',
      title: 'Re-protect complete: replication healthy',
      linkText: 'View execution',
      linkTo: link,
      persistent: false,
      timeout: 8000,
    });
    return;
  }

  switch (result) {
    case 'Succeeded': {
      const vmCount = getVMCount(execution);
      const duration = formatDuration(
        execution.status?.startTime,
        execution.status?.completionTime,
      );
      addToast({
        variant: 'success',
        title: `${modeLabel} completed: ${vmCount} VMs recovered in ${duration}`,
        linkText: 'View execution',
        linkTo: link,
        persistent: false,
        timeout: 15000,
      });
      break;
    }
    case 'PartiallySucceeded': {
      const failedCount = getFailedGroupCount(execution);
      addToast({
        variant: 'warning',
        title: `${modeLabel} partially succeeded: ${failedCount} DRGroup failed`,
        linkText: 'View Details',
        linkTo: link,
        persistent: true,
        timeout: 0,
      });
      break;
    }
    case 'Failed':
      addToast({
        variant: 'danger',
        title: `${modeLabel} failed for ${planName}`,
        linkText: 'View execution',
        linkTo: link,
        persistent: true,
        timeout: 0,
      });
      break;
  }
}

interface PrevState {
  result?: string;
  startTime?: string;
}

export function useExecutionNotifications(): void {
  const [executions, loaded] = useDRExecutions();
  const prevStates = useRef<Map<string, PrevState>>(new Map());
  const initialized = useRef(false);

  useEffect(() => {
    if (!loaded || !executions) return;

    if (!initialized.current) {
      executions.forEach((exec) => {
        prevStates.current.set(exec.metadata?.name ?? '', {
          result: exec.status?.result,
          startTime: exec.status?.startTime,
        });
      });
      initialized.current = true;
      return;
    }

    executions.forEach((exec) => {
      const name = exec.metadata?.name ?? '';
      const prev = prevStates.current.get(name);
      const curr = {
        result: exec.status?.result,
        startTime: exec.status?.startTime,
      };

      if (!prev && curr.startTime && !curr.result) {
        fireStartedToast(exec);
      } else if (!prev && curr.result) {
        fireCompletedToast(exec);
      } else if (prev && !prev.result && curr.result) {
        fireCompletedToast(exec);
      }

      prevStates.current.set(name, curr);
    });
  }, [executions, loaded]);
}
