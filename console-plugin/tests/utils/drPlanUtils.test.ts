import {
  getEffectivePhase,
  getReplicationHealth,
  getLastExecution,
  buildLatestExecutionMap,
  HEALTH_SORT_ORDER,
} from '../../src/utils/drPlanUtils';
import { getValidActions, isTransientPhase } from '../../src/utils/drPlanActions';
import { formatDuration, formatRelativeTime } from '../../src/utils/formatters';
import { DRExecution, DRPlan } from '../../src/models/types';

function makePlan(overrides: Partial<DRPlan['status']> = {}): DRPlan {
  return {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRPlan',
    metadata: { name: 'test', uid: '1', creationTimestamp: '' },
    spec: { maxConcurrentFailovers: 1, primarySite: 'a', secondarySite: 'b' },
    status: { phase: 'SteadyState', ...overrides },
  };
}

describe('getEffectivePhase', () => {
  it('returns SteadyState when no status', () => {
    const plan = makePlan();
    delete plan.status;
    expect(getEffectivePhase(plan)).toBe('SteadyState');
  });

  it('returns rest phase when no activeExecution', () => {
    expect(getEffectivePhase(makePlan({ phase: 'FailedOver' }))).toBe('FailedOver');
  });

  it('returns FailingOver from SteadyState with disaster activeExecution', () => {
    expect(
      getEffectivePhase(
        makePlan({
          phase: 'SteadyState',
          activeExecution: 'exec-1',
          activeExecutionMode: 'disaster',
        }),
      ),
    ).toBe('FailingOver');
  });

  it('returns FailingOver from SteadyState with planned_migration', () => {
    expect(
      getEffectivePhase(
        makePlan({
          phase: 'SteadyState',
          activeExecution: 'exec-1',
          activeExecutionMode: 'planned_migration',
        }),
      ),
    ).toBe('FailingOver');
  });

  it('returns Reprotecting from FailedOver with reprotect', () => {
    expect(
      getEffectivePhase(
        makePlan({
          phase: 'FailedOver',
          activeExecution: 'exec-1',
          activeExecutionMode: 'reprotect',
        }),
      ),
    ).toBe('Reprotecting');
  });

  it('returns FailingBack from DRedSteadyState with planned_migration', () => {
    expect(
      getEffectivePhase(
        makePlan({
          phase: 'DRedSteadyState',
          activeExecution: 'exec-1',
          activeExecutionMode: 'planned_migration',
        }),
      ),
    ).toBe('FailingBack');
  });

  it('returns Restoring from FailedBack with reprotect', () => {
    expect(
      getEffectivePhase(
        makePlan({
          phase: 'FailedBack',
          activeExecution: 'exec-1',
          activeExecutionMode: 'reprotect',
        }),
      ),
    ).toBe('Restoring');
  });

  it('returns SteadyState when phase is undefined', () => {
    expect(getEffectivePhase(makePlan({ phase: undefined }))).toBe('SteadyState');
  });
});

describe('getReplicationHealth', () => {
  it('returns Unknown when no conditions', () => {
    expect(getReplicationHealth(makePlan())).toEqual({ status: 'Unknown' });
  });

  it('returns Healthy when condition is True', () => {
    expect(
      getReplicationHealth(
        makePlan({
          conditions: [{ type: 'ReplicationHealthy', status: 'True', message: 'RPO: 12s' }],
        }),
      ),
    ).toEqual({ status: 'Healthy' });
  });

  it('returns Degraded when condition is False with Degraded reason', () => {
    expect(
      getReplicationHealth(
        makePlan({
          conditions: [
            { type: 'ReplicationHealthy', status: 'False', reason: 'Degraded', message: 'RPO: 60s' },
          ],
        }),
      ),
    ).toEqual({ status: 'Degraded' });
  });

  it('returns Error when condition is False without Degraded reason', () => {
    expect(
      getReplicationHealth(
        makePlan({
          conditions: [
            { type: 'ReplicationHealthy', status: 'False', reason: 'Error', message: 'driver fail' },
          ],
        }),
      ),
    ).toEqual({ status: 'Error' });
  });

  it('returns Unknown when condition status is Unknown', () => {
    expect(
      getReplicationHealth(
        makePlan({
          conditions: [{ type: 'ReplicationHealthy', status: 'Unknown' }],
        }),
      ),
    ).toEqual({ status: 'Unknown' });
  });
});

describe('getLastExecution', () => {
  const executions: DRExecution[] = [
    {
      apiVersion: 'soteria.io/v1alpha1',
      kind: 'DRExecution',
      metadata: { name: 'e1', uid: '1', creationTimestamp: '' },
      spec: { planName: 'plan-a', mode: 'disaster' },
      status: { startTime: '2026-04-20T10:00:00Z' },
    },
    {
      apiVersion: 'soteria.io/v1alpha1',
      kind: 'DRExecution',
      metadata: { name: 'e2', uid: '2', creationTimestamp: '' },
      spec: { planName: 'plan-a', mode: 'planned_migration' },
      status: { startTime: '2026-04-25T10:00:00Z' },
    },
    {
      apiVersion: 'soteria.io/v1alpha1',
      kind: 'DRExecution',
      metadata: { name: 'e3', uid: '3', creationTimestamp: '' },
      spec: { planName: 'plan-b', mode: 'reprotect' },
      status: { startTime: '2026-04-22T10:00:00Z' },
    },
  ];

  it('returns the most recent execution for the given plan', () => {
    const result = getLastExecution(executions, 'plan-a');
    expect(result?.metadata?.name).toBe('e2');
  });

  it('returns null when no executions match the plan', () => {
    expect(getLastExecution(executions, 'plan-c')).toBeNull();
  });

  it('returns null for empty executions array', () => {
    expect(getLastExecution([], 'plan-a')).toBeNull();
  });
});

describe('buildLatestExecutionMap', () => {
  const executions: DRExecution[] = [
    {
      apiVersion: 'soteria.io/v1alpha1',
      kind: 'DRExecution',
      metadata: { name: 'e1', uid: '1', creationTimestamp: '' },
      spec: { planName: 'plan-a', mode: 'disaster' },
      status: { startTime: '2026-04-20T10:00:00Z' },
    },
    {
      apiVersion: 'soteria.io/v1alpha1',
      kind: 'DRExecution',
      metadata: { name: 'e2', uid: '2', creationTimestamp: '' },
      spec: { planName: 'plan-a', mode: 'planned_migration' },
      status: { startTime: '2026-04-25T10:00:00Z' },
    },
    {
      apiVersion: 'soteria.io/v1alpha1',
      kind: 'DRExecution',
      metadata: { name: 'e3', uid: '3', creationTimestamp: '' },
      spec: { planName: 'plan-b', mode: 'reprotect' },
      status: { startTime: '2026-04-22T10:00:00Z' },
    },
  ];

  it('returns the latest execution per plan in a single pass', () => {
    const map = buildLatestExecutionMap(executions);
    expect(map.size).toBe(2);
    expect(map.get('plan-a')?.metadata?.name).toBe('e2');
    expect(map.get('plan-b')?.metadata?.name).toBe('e3');
  });

  it('returns an empty map for empty input', () => {
    expect(buildLatestExecutionMap([]).size).toBe(0);
  });

  it('skips executions without a planName', () => {
    const noSpec: DRExecution = {
      apiVersion: 'soteria.io/v1alpha1',
      kind: 'DRExecution',
      metadata: { name: 'orphan', uid: '4', creationTimestamp: '' },
      spec: { planName: '', mode: 'disaster' },
    };
    const map = buildLatestExecutionMap([noSpec]);
    expect(map.size).toBe(0);
  });

  it('agrees with getLastExecution for each plan', () => {
    const map = buildLatestExecutionMap(executions);
    expect(map.get('plan-a')).toEqual(getLastExecution(executions, 'plan-a'));
    expect(map.get('plan-b')).toEqual(getLastExecution(executions, 'plan-b'));
    expect(map.has('plan-c')).toBe(false);
  });
});

describe('HEALTH_SORT_ORDER', () => {
  it('orders Error < Degraded < Unknown < Healthy', () => {
    expect(HEALTH_SORT_ORDER['Error']).toBeLessThan(HEALTH_SORT_ORDER['Degraded']);
    expect(HEALTH_SORT_ORDER['Degraded']).toBeLessThan(HEALTH_SORT_ORDER['Unknown']);
    expect(HEALTH_SORT_ORDER['Unknown']).toBeLessThan(HEALTH_SORT_ORDER['Healthy']);
  });
});

describe('getValidActions', () => {
  it('returns Failover + Planned Migration for SteadyState', () => {
    const plan = makePlan({ phase: 'SteadyState' });
    const actions = getValidActions(plan);
    expect(actions.map((a) => a.key)).toEqual(['failover', 'planned_migration']);
    expect(actions[0].isDanger).toBe(true);
  });

  it('returns Reprotect for FailedOver', () => {
    const plan = makePlan({ phase: 'FailedOver' });
    expect(getValidActions(plan).map((a) => a.key)).toEqual(['reprotect']);
  });

  it('returns Failback and Planned Migration for DRedSteadyState', () => {
    const plan = makePlan({ phase: 'DRedSteadyState' });
    expect(getValidActions(plan).map((a) => a.key)).toEqual(['failback', 'planned_failback']);
  });

  it('returns Restore for FailedBack', () => {
    const plan = makePlan({ phase: 'FailedBack' });
    expect(getValidActions(plan).map((a) => a.key)).toEqual(['restore']);
  });

  it('returns empty actions for transient phases', () => {
    const plan = makePlan({
      phase: 'SteadyState',
      activeExecution: 'exec-1',
      activeExecutionMode: 'disaster',
    });
    expect(getValidActions(plan)).toEqual([]);
  });
});

describe('isTransientPhase', () => {
  it.each(['FailingOver', 'Reprotecting', 'FailingBack', 'Restoring'] as const)(
    'returns true for %s',
    (phase) => expect(isTransientPhase(phase)).toBe(true),
  );

  it.each(['SteadyState', 'FailedOver', 'DRedSteadyState', 'FailedBack'] as const)(
    'returns false for %s',
    (phase) => expect(isTransientPhase(phase)).toBe(false),
  );
});

describe('formatDuration', () => {
  it('returns empty for missing start', () => expect(formatDuration(undefined, '2026-01-01')).toBe(''));
  it('returns empty for missing end', () => expect(formatDuration('2026-01-01', undefined)).toBe(''));
  it('formats seconds', () =>
    expect(formatDuration('2026-01-01T00:00:00Z', '2026-01-01T00:00:30Z')).toBe('30s'));
  it('formats minutes and seconds', () =>
    expect(formatDuration('2026-01-01T00:00:00Z', '2026-01-01T00:02:34Z')).toBe('2m 34s'));
  it('formats hours and minutes', () =>
    expect(formatDuration('2026-01-01T00:00:00Z', '2026-01-01T01:15:00Z')).toBe('1h 15m'));
});

describe('formatRelativeTime', () => {
  it('returns empty for undefined', () => expect(formatRelativeTime(undefined)).toBe(''));
  it('returns "just now" for recent time', () => {
    const now = new Date();
    expect(formatRelativeTime(now.toISOString())).toBe('just now');
  });
  it('returns minutes ago', () => {
    const fiveMinAgo = new Date(Date.now() - 5 * 60 * 1000).toISOString();
    expect(formatRelativeTime(fiveMinAgo)).toBe('5 min ago');
  });
  it('returns hours ago', () => {
    const threeHoursAgo = new Date(Date.now() - 3 * 60 * 60 * 1000).toISOString();
    expect(formatRelativeTime(threeHoursAgo)).toBe('3h ago');
  });
  it('returns days ago', () => {
    const twoDaysAgo = new Date(Date.now() - 2 * 24 * 60 * 60 * 1000).toISOString();
    expect(formatRelativeTime(twoDaysAgo)).toBe('2d ago');
  });
});
