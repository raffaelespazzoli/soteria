import {
  getEffectivePhase,
  getReplicationHealth,
  getSitesInSync,
  parseSiteDiscoveryDelta,
  getLastExecution,
  buildLatestExecutionMap,
  findActiveExecution,
  buildActiveExecMap,
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
    spec: { maxConcurrentFailovers: 1, primarySite: 'a', secondarySite: 'b', volumeReplicationDriver: 'noop' },
    status: { phase: 'SteadyState', ...overrides },
  };
}

function makeExec(mode: 'disaster' | 'planned_migration' | 'reprotect'): DRExecution {
  return {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRExecution',
    metadata: { name: 'exec-1', uid: 'u1', creationTimestamp: '' },
    spec: { planName: 'test', mode },
    status: { phase: 'Executing', isActive: true },
  };
}

describe('getEffectivePhase', () => {
  it('returns SteadyState when no status', () => {
    const plan = makePlan();
    delete plan.status;
    expect(getEffectivePhase(plan)).toBe('SteadyState');
  });

  it('returns rest phase when no activeExec provided', () => {
    expect(getEffectivePhase(makePlan({ phase: 'FailedOver' }))).toBe('FailedOver');
  });

  it('returns rest phase when activeExec is undefined', () => {
    expect(getEffectivePhase(makePlan({ phase: 'FailedOver' }), undefined)).toBe('FailedOver');
  });

  it('returns FailingOver from SteadyState with disaster exec', () => {
    expect(getEffectivePhase(makePlan({ phase: 'SteadyState' }), makeExec('disaster'))).toBe('FailingOver');
  });

  it('returns FailingOver from SteadyState with planned_migration exec', () => {
    expect(getEffectivePhase(makePlan({ phase: 'SteadyState' }), makeExec('planned_migration'))).toBe('FailingOver');
  });

  it('returns Reprotecting from FailedOver with reprotect exec', () => {
    expect(getEffectivePhase(makePlan({ phase: 'FailedOver' }), makeExec('reprotect'))).toBe('Reprotecting');
  });

  it('returns FailingBack from DRedSteadyState with planned_migration exec', () => {
    expect(getEffectivePhase(makePlan({ phase: 'DRedSteadyState' }), makeExec('planned_migration'))).toBe('FailingBack');
  });

  it('returns Restoring from FailedBack with reprotect exec', () => {
    expect(getEffectivePhase(makePlan({ phase: 'FailedBack' }), makeExec('reprotect'))).toBe('Restoring');
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

describe('findActiveExecution', () => {
  it('returns the first execution with isActive === true', () => {
    const execs: DRExecution[] = [
      { apiVersion: 'soteria.io/v1alpha1', kind: 'DRExecution', metadata: { name: 'e1', uid: '1', creationTimestamp: '' }, spec: { planName: 'plan-a', mode: 'disaster' }, status: { isActive: false, phase: 'Succeeded', result: 'Succeeded' } },
      { apiVersion: 'soteria.io/v1alpha1', kind: 'DRExecution', metadata: { name: 'e2', uid: '2', creationTimestamp: '' }, spec: { planName: 'plan-a', mode: 'disaster' }, status: { isActive: true, phase: 'Executing' } },
    ];
    expect(findActiveExecution(execs)?.metadata?.name).toBe('e2');
  });

  it('returns undefined when no active execution exists', () => {
    const execs: DRExecution[] = [
      { apiVersion: 'soteria.io/v1alpha1', kind: 'DRExecution', metadata: { name: 'e1', uid: '1', creationTimestamp: '' }, spec: { planName: 'plan-a', mode: 'disaster' }, status: { isActive: false, phase: 'Succeeded', result: 'Succeeded' } },
    ];
    expect(findActiveExecution(execs)).toBeUndefined();
  });

  it('returns undefined for empty array', () => {
    expect(findActiveExecution([])).toBeUndefined();
  });

  it('handles execution with undefined status', () => {
    const execs: DRExecution[] = [
      { apiVersion: 'soteria.io/v1alpha1', kind: 'DRExecution', metadata: { name: 'e1', uid: '1', creationTimestamp: '' }, spec: { planName: 'plan-a', mode: 'disaster' } },
    ];
    expect(findActiveExecution(execs)).toBeUndefined();
  });
});

describe('buildActiveExecMap', () => {
  it('builds a planName → active DRExecution map', () => {
    const execs: DRExecution[] = [
      { apiVersion: 'soteria.io/v1alpha1', kind: 'DRExecution', metadata: { name: 'e1', uid: '1', creationTimestamp: '' }, spec: { planName: 'plan-a', mode: 'disaster' }, status: { isActive: false, phase: 'Succeeded', result: 'Succeeded' } },
      { apiVersion: 'soteria.io/v1alpha1', kind: 'DRExecution', metadata: { name: 'e2', uid: '2', creationTimestamp: '' }, spec: { planName: 'plan-a', mode: 'disaster' }, status: { isActive: true, phase: 'Executing' } },
      { apiVersion: 'soteria.io/v1alpha1', kind: 'DRExecution', metadata: { name: 'e3', uid: '3', creationTimestamp: '' }, spec: { planName: 'plan-b', mode: 'reprotect' }, status: { isActive: true, phase: 'Pending' } },
    ];
    const map = buildActiveExecMap(execs);
    expect(map.size).toBe(2);
    expect(map.get('plan-a')?.metadata?.name).toBe('e2');
    expect(map.get('plan-b')?.metadata?.name).toBe('e3');
  });

  it('returns empty map when no active executions', () => {
    const execs: DRExecution[] = [
      { apiVersion: 'soteria.io/v1alpha1', kind: 'DRExecution', metadata: { name: 'e1', uid: '1', creationTimestamp: '' }, spec: { planName: 'plan-a', mode: 'disaster' }, status: { isActive: false, phase: 'Succeeded', result: 'Succeeded' } },
    ];
    expect(buildActiveExecMap(execs).size).toBe(0);
  });

  it('returns empty map for empty input', () => {
    expect(buildActiveExecMap([]).size).toBe(0);
  });

  it('skips executions without planName', () => {
    const execs: DRExecution[] = [
      { apiVersion: 'soteria.io/v1alpha1', kind: 'DRExecution', metadata: { name: 'e1', uid: '1', creationTimestamp: '' }, spec: { planName: '', mode: 'disaster' }, status: { isActive: true, phase: 'Executing' } },
    ];
    expect(buildActiveExecMap(execs).size).toBe(0);
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
    const actions = getValidActions('SteadyState');
    expect(actions.map((a) => a.key)).toEqual(['failover', 'planned_migration']);
    expect(actions[0].isDanger).toBe(true);
  });

  it('returns Reprotect for FailedOver', () => {
    expect(getValidActions('FailedOver').map((a) => a.key)).toEqual(['reprotect']);
  });

  it('returns Failback and Planned Migration for DRedSteadyState', () => {
    expect(getValidActions('DRedSteadyState').map((a) => a.key)).toEqual(['failback', 'planned_failback']);
  });

  it('returns Restore for FailedBack', () => {
    expect(getValidActions('FailedBack').map((a) => a.key)).toEqual(['restore']);
  });

  it('returns empty actions for transient phases', () => {
    expect(getValidActions('FailingOver')).toEqual([]);
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

describe('getSitesInSync', () => {
  it('returns inSync: true when no conditions exist', () => {
    expect(getSitesInSync(makePlan())).toEqual({ inSync: true });
  });

  it('returns inSync: true when SitesInSync condition is True', () => {
    expect(
      getSitesInSync(
        makePlan({
          conditions: [{ type: 'SitesInSync', status: 'True', reason: 'VMsAgreed' }],
        }),
      ),
    ).toEqual({ inSync: true });
  });

  it('returns inSync: false with reason and message when SitesInSync is False', () => {
    const result = getSitesInSync(
      makePlan({
        conditions: [
          {
            type: 'SitesInSync',
            status: 'False',
            reason: 'VMsMismatch',
            message: 'VMs on primary but not secondary: [ns/vm-a]; VMs on secondary but not primary: [ns/vm-c]',
          },
        ],
      }),
    );
    expect(result).toEqual({
      inSync: false,
      reason: 'VMsMismatch',
      message: 'VMs on primary but not secondary: [ns/vm-a]; VMs on secondary but not primary: [ns/vm-c]',
    });
  });

  it('returns inSync: true when condition is absent (backward compat)', () => {
    expect(
      getSitesInSync(
        makePlan({
          conditions: [{ type: 'ReplicationHealthy', status: 'True' }],
        }),
      ),
    ).toEqual({ inSync: true });
  });
});

describe('parseSiteDiscoveryDelta', () => {
  it('parses structured delta message correctly', () => {
    const msg =
      'VMs on primary but not secondary: [ns/vm-a, ns/vm-b]; VMs on secondary but not primary: [ns/vm-c]';
    expect(parseSiteDiscoveryDelta(msg)).toEqual({
      primaryOnly: ['ns/vm-a', 'ns/vm-b'],
      secondaryOnly: ['ns/vm-c'],
      primaryMoreCount: 0,
      secondaryMoreCount: 0,
    });
  });

  it('handles only-primary mismatch', () => {
    const msg = 'VMs on primary but not secondary: [ns/vm-x]';
    expect(parseSiteDiscoveryDelta(msg)).toEqual({
      primaryOnly: ['ns/vm-x'],
      secondaryOnly: [],
      primaryMoreCount: 0,
      secondaryMoreCount: 0,
    });
  });

  it('handles only-secondary mismatch', () => {
    const msg = 'VMs on secondary but not primary: [ns/vm-y, ns/vm-z]';
    expect(parseSiteDiscoveryDelta(msg)).toEqual({
      primaryOnly: [],
      secondaryOnly: ['ns/vm-y', 'ns/vm-z'],
      primaryMoreCount: 0,
      secondaryMoreCount: 0,
    });
  });

  it('handles "... and N more" suffix and exposes the extra count', () => {
    const msg =
      'VMs on primary but not secondary: [ns/vm-1, ns/vm-2, ... and 5 more]; VMs on secondary but not primary: [ns/vm-3, ... and 3 more]';
    const result = parseSiteDiscoveryDelta(msg);
    expect(result.primaryOnly).toEqual(['ns/vm-1', 'ns/vm-2']);
    expect(result.primaryMoreCount).toBe(5);
    expect(result.secondaryOnly).toEqual(['ns/vm-3']);
    expect(result.secondaryMoreCount).toBe(3);
  });

  it('returns empty arrays and zero counts for undefined message', () => {
    expect(parseSiteDiscoveryDelta(undefined)).toEqual({
      primaryOnly: [],
      secondaryOnly: [],
      primaryMoreCount: 0,
      secondaryMoreCount: 0,
    });
  });

  it('returns empty arrays and zero counts for empty string', () => {
    expect(parseSiteDiscoveryDelta('')).toEqual({
      primaryOnly: [],
      secondaryOnly: [],
      primaryMoreCount: 0,
      secondaryMoreCount: 0,
    });
  });

  it('returns empty arrays and zero counts for malformed message', () => {
    expect(parseSiteDiscoveryDelta('Site dc-east discovered 0 VMs')).toEqual({
      primaryOnly: [],
      secondaryOnly: [],
      primaryMoreCount: 0,
      secondaryMoreCount: 0,
    });
  });
});
