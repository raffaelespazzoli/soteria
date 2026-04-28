import { getPreflightData } from '../../src/hooks/usePreflightData';
import { DRExecution, DRPlan } from '../../src/models/types';

function makePlan(overrides: Partial<DRPlan['status']> = {}): DRPlan {
  return {
    apiVersion: 'soteria.io/v1alpha1',
    kind: 'DRPlan',
    metadata: { name: 'erp-full-stack', uid: '1', creationTimestamp: '' },
    spec: {
      waveLabel: 'soteria.io/wave',
      maxConcurrentFailovers: 4,
      primarySite: 'dc1-prod',
      secondarySite: 'dc2-prod',
    },
    status: {
      phase: 'SteadyState',
      activeSite: 'dc1-prod',
      discoveredVMCount: 12,
      waves: [
        { waveKey: '1', vms: [] },
        { waveKey: '2', vms: [] },
        { waveKey: '3', vms: [] },
      ],
      conditions: [
        {
          type: 'ReplicationHealthy',
          status: 'True',
          reason: 'Healthy',
          message: 'RPO: 47s',
        },
      ],
      ...overrides,
    },
  };
}

const mockExecution: DRExecution = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRExecution',
  metadata: { name: 'erp-full-stack-failover-1', uid: '10', creationTimestamp: '' },
  spec: { planName: 'erp-full-stack', mode: 'disaster' },
  status: {
    result: 'Succeeded',
    startTime: '2026-04-24T10:00:00Z',
    completionTime: '2026-04-24T10:18:00Z',
  },
};

describe('getPreflightData', () => {
  describe('VM and wave counts', () => {
    it('returns VM count and wave count from plan status', () => {
      const data = getPreflightData(makePlan(), 'failover', []);
      expect(data.vmCount).toBe(12);
      expect(data.waveCount).toBe(3);
    });

    it('returns zero counts when plan status is empty', () => {
      const plan = makePlan();
      delete plan.status!.discoveredVMCount;
      delete plan.status!.waves;
      const data = getPreflightData(plan, 'failover', []);
      expect(data.vmCount).toBe(0);
      expect(data.waveCount).toBe(0);
    });
  });

  describe('RPO estimation', () => {
    it('computes estimated RPO from plan health for disaster failover', () => {
      const data = getPreflightData(makePlan(), 'failover', []);
      expect(data.estimatedRPO).toBe('47 seconds');
      expect(data.estimatedRPOSeconds).toBe(47);
    });

    it('returns Unknown RPO when no replication health for failover', () => {
      const plan = makePlan({ conditions: [] });
      const data = getPreflightData(plan, 'failover', []);
      expect(data.estimatedRPO).toBe('Unknown');
      expect(data.estimatedRPOSeconds).toBeNull();
    });

    it('returns "0 — guaranteed" RPO for planned migration', () => {
      const data = getPreflightData(makePlan(), 'planned_migration', []);
      expect(data.estimatedRPO).toMatch(/0 — guaranteed/);
    });

    it('returns "0 — guaranteed" RPO for failback', () => {
      const data = getPreflightData(makePlan(), 'failback', []);
      expect(data.estimatedRPO).toMatch(/0 — guaranteed/);
    });

    it('returns "0 — guaranteed" RPO for planned_failback', () => {
      const data = getPreflightData(makePlan(), 'planned_failback', []);
      expect(data.estimatedRPO).toMatch(/0 — guaranteed/);
    });

    it('returns "N/A" RPO for reprotect', () => {
      const data = getPreflightData(makePlan(), 'reprotect', []);
      expect(data.estimatedRPO).toMatch(/N\/A/);
    });

    it('returns "N/A" RPO for restore', () => {
      const data = getPreflightData(makePlan(), 'restore', []);
      expect(data.estimatedRPO).toMatch(/N\/A/);
    });

    it('formats RPO in minutes when >= 60 seconds', () => {
      const plan = makePlan({
        conditions: [
          { type: 'ReplicationHealthy', status: 'True', reason: 'Healthy', message: 'RPO: 120s' },
        ],
      });
      const data = getPreflightData(plan, 'failover', []);
      expect(data.estimatedRPO).toBe('2 minutes');
    });

    it('formats RPO in hours when >= 3600 seconds', () => {
      const plan = makePlan({
        conditions: [
          { type: 'ReplicationHealthy', status: 'True', reason: 'Healthy', message: 'RPO: 7200s' },
        ],
      });
      const data = getPreflightData(plan, 'failover', []);
      expect(data.estimatedRPO).toBe('2 hours');
    });

    it('uses singular form for 1 second', () => {
      const plan = makePlan({
        conditions: [
          { type: 'ReplicationHealthy', status: 'True', reason: 'Healthy', message: 'RPO: 1s' },
        ],
      });
      const data = getPreflightData(plan, 'failover', []);
      expect(data.estimatedRPO).toBe('1 second');
    });
  });

  describe('RTO estimation', () => {
    it('computes estimated RTO from last execution duration', () => {
      const data = getPreflightData(makePlan(), 'failover', [mockExecution]);
      expect(data.estimatedRTO).toBe('~18 min based on last execution');
    });

    it('returns "Unknown — no previous execution" when no executions', () => {
      const data = getPreflightData(makePlan(), 'failover', []);
      expect(data.estimatedRTO).toBe('Unknown — no previous execution');
    });

    it('returns "Unknown — no previous execution" when execution has no completion time', () => {
      const exec: DRExecution = {
        ...mockExecution,
        status: { ...mockExecution.status, completionTime: undefined },
      };
      const data = getPreflightData(makePlan(), 'failover', [exec]);
      expect(data.estimatedRTO).toBe('Unknown — no previous execution');
    });
  });

  describe('action summary text', () => {
    it('generates correct summary for failover', () => {
      const data = getPreflightData(makePlan(), 'failover', []);
      expect(data.actionSummary).toBe(
        'Force-promote volumes on dc2-prod, start VMs wave by wave',
      );
    });

    it('generates correct summary for planned migration with Step 0 framing', () => {
      const data = getPreflightData(makePlan(), 'planned_migration', []);
      expect(data.actionSummary).toMatch(/^Step 0:/);
      expect(data.actionSummary).toContain('Stop VMs on dc1-prod');
      expect(data.actionSummary).toContain('promote volumes on dc2-prod');
    });

    it('generates correct summary for reprotect', () => {
      const data = getPreflightData(makePlan(), 'reprotect', []);
      expect(data.actionSummary).toContain('Demote volumes');
      expect(data.actionSummary).toContain('replication resync');
    });

    it('generates correct summary for failback with Step 0 framing', () => {
      const data = getPreflightData(makePlan(), 'failback', []);
      expect(data.actionSummary).toMatch(/^Step 0:/);
      expect(data.actionSummary).toContain('Stop VMs on dc1-prod');
      expect(data.actionSummary).toContain('promote volumes on dc1-prod');
    });

    it('generates correct summary for restore', () => {
      const data = getPreflightData(makePlan(), 'restore', []);
      expect(data.actionSummary).toContain('Demote volumes');
      expect(data.actionSummary).toContain('replication resync');
    });
  });

  describe('site information', () => {
    it('extracts primary, secondary, and active site from plan', () => {
      const data = getPreflightData(makePlan(), 'failover', []);
      expect(data.primarySite).toBe('dc1-prod');
      expect(data.secondarySite).toBe('dc2-prod');
      expect(data.activeSite).toBe('dc1-prod');
    });
  });

  describe('capacity assessment', () => {
    it('returns "unknown" when no preflight data', () => {
      const data = getPreflightData(makePlan(), 'failover', []);
      expect(data.capacityAssessment).toBe('unknown');
    });

    it('returns "warning" when preflight has warnings', () => {
      const plan = makePlan({
        preflight: { totalVMs: 12, warnings: ['Low capacity on dc2-prod'] },
      });
      const data = getPreflightData(plan, 'failover', []);
      expect(data.capacityAssessment).toBe('warning');
    });

    it('returns "sufficient" when preflight exists with no warnings', () => {
      const plan = makePlan({
        preflight: { totalVMs: 12 },
      });
      const data = getPreflightData(plan, 'failover', []);
      expect(data.capacityAssessment).toBe('sufficient');
    });
  });
});
