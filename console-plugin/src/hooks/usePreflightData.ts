import { DRExecution, DRPlan } from '../models/types';
import { getReplicationHealth, getLastExecution } from '../utils/drPlanUtils';

export interface PreflightData {
  vmCount: number;
  waveCount: number;
  estimatedRPO: string;
  estimatedRPOSeconds: number | null;
  estimatedRTO: string;
  capacityAssessment: 'sufficient' | 'warning' | 'unknown';
  actionSummary: string;
  primarySite: string;
  secondarySite: string;
  activeSite: string;
}

function formatRPOHuman(seconds: number): string {
  if (seconds < 60) return `${seconds} second${seconds !== 1 ? 's' : ''}`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} minute${minutes !== 1 ? 's' : ''}`;
  const hours = Math.floor(minutes / 60);
  return `${hours} hour${hours !== 1 ? 's' : ''}`;
}

function getEstimatedRPO(action: string, rpoSeconds: number | null): string {
  if (action === 'failover') {
    return rpoSeconds !== null ? formatRPOHuman(rpoSeconds) : 'Unknown';
  }
  if (['planned_migration', 'failback', 'planned_failback'].includes(action)) {
    return '0 — guaranteed (both DCs up, final sync before promote)';
  }
  if (['reprotect', 'restore'].includes(action)) {
    return 'N/A — no data movement, establishes reverse replication';
  }
  return 'Unknown';
}

function getEstimatedRTO(lastExec: DRExecution | null): string {
  if (!lastExec?.status?.startTime || !lastExec?.status?.completionTime) {
    return 'Unknown — no previous execution';
  }
  const ms =
    new Date(lastExec.status.completionTime).getTime() -
    new Date(lastExec.status.startTime).getTime();
  if (ms <= 0) return 'Unknown';
  const minutes = Math.round(ms / 60000);
  if (minutes < 1) return '~<1 min based on last execution';
  return `~${minutes} min based on last execution`;
}

function getActionSummary(
  action: string,
  primarySite: string,
  secondarySite: string,
  activeSite: string,
): string {
  switch (action) {
    case 'failover':
      return `Force-promote volumes on ${secondarySite}, start VMs wave by wave`;
    case 'planned_migration':
      return `Step 0: Stop VMs on ${activeSite} → wait for final replication sync → promote volumes on ${secondarySite} → start VMs wave by wave`;
    case 'reprotect':
      return 'Demote volumes on old active site, initiate replication resync, monitor until healthy';
    case 'failback':
    case 'planned_failback':
      return `Step 0: Stop VMs on ${activeSite} → wait for final replication sync → promote volumes on ${primarySite} → start VMs wave by wave`;
    case 'restore':
      return 'Demote volumes on old active site, initiate replication resync, monitor until healthy';
    default:
      return '';
  }
}

export function getPreflightData(
  plan: DRPlan,
  action: string,
  executions: DRExecution[],
): PreflightData {
  const planName = plan.metadata?.name ?? '';
  const health = getReplicationHealth(plan);
  const lastExec = getLastExecution(executions, planName);

  const primarySite = plan.spec?.primarySite ?? 'Primary';
  const secondarySite = plan.spec?.secondarySite ?? 'Secondary';
  const activeSite = plan.status?.activeSite ?? primarySite;

  const preflight = plan.status?.preflight;
  let capacity: 'sufficient' | 'warning' | 'unknown' = 'unknown';
  if (preflight) {
    capacity = (preflight.warnings?.length ?? 0) > 0 ? 'warning' : 'sufficient';
  }

  return {
    vmCount: plan.status?.discoveredVMCount ?? 0,
    waveCount: plan.status?.waves?.length ?? 0,
    estimatedRPO: getEstimatedRPO(action, health.rpoSeconds),
    estimatedRPOSeconds: health.rpoSeconds,
    estimatedRTO: getEstimatedRTO(lastExec),
    capacityAssessment: capacity,
    actionSummary: getActionSummary(action, primarySite, secondarySite, activeSite),
    primarySite,
    secondarySite,
    activeSite,
  };
}
