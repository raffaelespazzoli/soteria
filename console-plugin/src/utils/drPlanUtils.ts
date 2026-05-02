import { DRExecution, DRPlan } from '../models/types';

export type RestPhase = 'SteadyState' | 'FailedOver' | 'DRedSteadyState' | 'FailedBack';
export type TransientPhase = 'FailingOver' | 'Reprotecting' | 'FailingBack' | 'Restoring';
export type EffectivePhase = RestPhase | TransientPhase;

export type ReplicationHealthStatus = 'Healthy' | 'Degraded' | 'Syncing' | 'NotReplicating' | 'Error' | 'Unknown';

export interface ReplicationHealth {
  status: ReplicationHealthStatus;
}

/**
 * Derives the effective phase from a DRPlan. Rest states are stored on
 * status.phase; transient states are derived from activeExecution + activeExecutionMode.
 * Mirrors the Go EffectivePhase() helper from pkg/engine/.
 */
export function getEffectivePhase(plan: DRPlan): EffectivePhase {
  const phase = plan.status?.phase;
  if (!plan.status?.activeExecution) return (phase as RestPhase) ?? 'SteadyState';
  const mode = plan.status.activeExecutionMode;
  switch (phase) {
    case 'SteadyState':
      return mode === 'planned_migration' || mode === 'disaster' ? 'FailingOver' : 'SteadyState';
    case 'FailedOver':
      return mode === 'reprotect' ? 'Reprotecting' : 'FailedOver';
    case 'DRedSteadyState':
      return mode === 'planned_migration' || mode === 'disaster'
        ? 'FailingBack'
        : 'DRedSteadyState';
    case 'FailedBack':
      return mode === 'reprotect' ? 'Restoring' : 'FailedBack';
    default:
      return (phase as RestPhase) ?? 'SteadyState';
  }
}

/**
 * Extracts replication health from the DRPlan's ReplicationHealthy condition.
 */
export function getReplicationHealth(plan: DRPlan): ReplicationHealth {
  const condition = plan.status?.conditions?.find((c) => c.type === 'ReplicationHealthy');
  if (!condition) return { status: 'Unknown' };

  switch (condition.status) {
    case 'True':
      return { status: 'Healthy' };
    case 'False':
      return { status: condition.reason === 'Degraded' ? 'Degraded' : 'Error' };
    default:
      return { status: 'Unknown' };
  }
}

/**
 * Finds the most recent DRExecution for a given plan name.
 */
export function getLastExecution(executions: DRExecution[], planName: string): DRExecution | null {
  return (
    executions
      .filter((e) => e.spec?.planName === planName)
      .sort((a, b) => {
        const timeA = new Date(a.status?.startTime ?? 0).getTime();
        const timeB = new Date(b.status?.startTime ?? 0).getTime();
        return timeB - timeA;
      })[0] ?? null
  );
}

/**
 * Builds a Map of planName → most-recent DRExecution in a single O(E) pass.
 * Use this when you need the latest execution for many plans at once (e.g. the
 * dashboard) to avoid the O(P * E log E) cost of calling getLastExecution per plan.
 */
export function buildLatestExecutionMap(executions: DRExecution[]): Map<string, DRExecution> {
  const map = new Map<string, DRExecution>();
  for (const exec of executions) {
    const planName = exec.spec?.planName;
    if (!planName) continue;
    const existing = map.get(planName);
    if (!existing) {
      map.set(planName, exec);
      continue;
    }
    const existingTime = new Date(existing.status?.startTime ?? 0).getTime();
    const newTime = new Date(exec.status?.startTime ?? 0).getTime();
    if (newTime > existingTime) {
      map.set(planName, exec);
    }
  }
  return map;
}

/** Sort order for replication health: problems surface first. */
export const HEALTH_SORT_ORDER: Record<string, number> = {
  Error: 0,
  Degraded: 1,
  Syncing: 2,
  Unknown: 3,
  NotReplicating: 4,
  Healthy: 5,
};

// --- SitesInSync helpers ---

export interface SitesInSyncStatus {
  inSync: boolean;
  reason?: string;
  message?: string;
}

/**
 * Extracts the SitesInSync status from the DRPlan's conditions.
 * Returns { inSync: true } when the condition is absent (backward compat —
 * plans without the condition are not blocked).
 */
export function getSitesInSync(plan: DRPlan): SitesInSyncStatus {
  const condition = plan.status?.conditions?.find((c) => c.type === 'SitesInSync');
  if (!condition) return { inSync: true };
  if (condition.status === 'True') return { inSync: true };
  return { inSync: false, reason: condition.reason, message: condition.message };
}

export interface SiteDiscoveryDelta {
  primaryOnly: string[];
  secondaryOnly: string[];
}

/**
 * Parses the structured delta message from the SitesInSync condition.
 * Expected format:
 *   "VMs on primary but not secondary: [ns/vm-a, ns/vm-b]; VMs on secondary but not primary: [ns/vm-c]"
 * Handles "... and N more" suffixes and malformed messages gracefully.
 */
export function parseSiteDiscoveryDelta(message: string | undefined): SiteDiscoveryDelta & { primaryMoreCount: number; secondaryMoreCount: number } {
  if (!message) return { primaryOnly: [], secondaryOnly: [], primaryMoreCount: 0, secondaryMoreCount: 0 };

  const primaryOnly: string[] = [];
  const secondaryOnly: string[] = [];
  let primaryMoreCount = 0;
  let secondaryMoreCount = 0;

  const primaryMatch = message.match(/VMs on primary but not secondary:\s*\[([^\]]*)\]/);
  if (primaryMatch?.[1]) {
    const parts = primaryMatch[1].split(',').map((s) => s.trim());
    for (const part of parts) {
      const moreMatch = part.match(/^\.\.\.\s*and\s+(\d+)\s+more$/);
      if (moreMatch) {
        primaryMoreCount = parseInt(moreMatch[1], 10);
      } else if (part && !part.startsWith('...')) {
        primaryOnly.push(part);
      }
    }
  }

  const secondaryMatch = message.match(/VMs on secondary but not primary:\s*\[([^\]]*)\]/);
  if (secondaryMatch?.[1]) {
    const parts = secondaryMatch[1].split(',').map((s) => s.trim());
    for (const part of parts) {
      const moreMatch = part.match(/^\.\.\.\s*and\s+(\d+)\s+more$/);
      if (moreMatch) {
        secondaryMoreCount = parseInt(moreMatch[1], 10);
      } else if (part && !part.startsWith('...')) {
        secondaryOnly.push(part);
      }
    }
  }

  return { primaryOnly, secondaryOnly, primaryMoreCount, secondaryMoreCount };
}
