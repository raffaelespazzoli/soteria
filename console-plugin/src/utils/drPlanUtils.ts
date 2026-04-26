import { DRExecution, DRPlan } from '../models/types';

export type RestPhase = 'SteadyState' | 'FailedOver' | 'DRedSteadyState' | 'FailedBack';
export type TransientPhase = 'FailingOver' | 'Reprotecting' | 'FailingBack' | 'Restoring';
export type EffectivePhase = RestPhase | TransientPhase;

export type ReplicationHealthStatus = 'Healthy' | 'Degraded' | 'Error' | 'Unknown';

export interface ReplicationHealth {
  status: ReplicationHealthStatus;
  rpoSeconds: number | null;
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
  if (!condition) return { status: 'Unknown', rpoSeconds: null };

  const rpoStr = condition.message?.match(/RPO: (\d+)s/)?.[1];
  const rpoSeconds = rpoStr ? parseInt(rpoStr, 10) : null;

  switch (condition.status) {
    case 'True':
      return { status: 'Healthy', rpoSeconds };
    case 'False':
      return { status: condition.reason === 'Degraded' ? 'Degraded' : 'Error', rpoSeconds };
    default:
      return { status: 'Unknown', rpoSeconds: null };
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
  Unknown: 2,
  Healthy: 3,
};
