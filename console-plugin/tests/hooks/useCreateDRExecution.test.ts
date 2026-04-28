import { useCreateDRExecution } from '../../src/hooks/useCreateDRExecution';
import { ACTION_CONFIG, resolveActionKey } from '../../src/utils/drPlanActions';

const mockK8sCreate = jest.fn();

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  k8sCreate: (...args: unknown[]) => mockK8sCreate(...args),
}));

jest.mock('react', () => {
  const actualReact = jest.requireActual('react');
  return {
    ...actualReact,
    useState: jest.fn(),
    useCallback: jest.fn((fn: unknown) => fn),
  };
});

describe('useCreateDRExecution', () => {
  let setIsCreating: jest.Mock;
  let setError: jest.Mock;

  beforeEach(() => {
    mockK8sCreate.mockReset();
    setIsCreating = jest.fn();
    setError = jest.fn();

    const { useState } = jest.requireMock('react');
    let callIndex = 0;
    useState.mockImplementation((init: unknown) => {
      callIndex++;
      if (callIndex % 2 === 1) return [false, setIsCreating];
      return [undefined, setError];
    });
  });

  it('maps failover action to disaster mode', async () => {
    mockK8sCreate.mockResolvedValue({ metadata: { name: 'test' } });
    const { create } = useCreateDRExecution();
    await create('erp-plan', 'failover');
    expect(mockK8sCreate.mock.calls[0][0].data.spec.mode).toBe('disaster');
  });

  it('maps planned_migration action to planned_migration mode', async () => {
    mockK8sCreate.mockResolvedValue({ metadata: { name: 'test' } });
    const { create } = useCreateDRExecution();
    await create('erp-plan', 'planned_migration');
    expect(mockK8sCreate.mock.calls[0][0].data.spec.mode).toBe('planned_migration');
  });

  it('maps reprotect action to reprotect mode', async () => {
    mockK8sCreate.mockResolvedValue({ metadata: { name: 'test' } });
    const { create } = useCreateDRExecution();
    await create('erp-plan', 'reprotect');
    expect(mockK8sCreate.mock.calls[0][0].data.spec.mode).toBe('reprotect');
  });

  it('maps failback action to planned_migration mode', async () => {
    mockK8sCreate.mockResolvedValue({ metadata: { name: 'test' } });
    const { create } = useCreateDRExecution();
    await create('erp-plan', 'failback');
    expect(mockK8sCreate.mock.calls[0][0].data.spec.mode).toBe('planned_migration');
  });

  it('maps restore action to reprotect mode', async () => {
    mockK8sCreate.mockResolvedValue({ metadata: { name: 'test' } });
    const { create } = useCreateDRExecution();
    await create('erp-plan', 'restore');
    expect(mockK8sCreate.mock.calls[0][0].data.spec.mode).toBe('reprotect');
  });

  it('generates resource name with plan name and action', async () => {
    mockK8sCreate.mockResolvedValue({ metadata: { name: 'test' } });
    const { create } = useCreateDRExecution();
    await create('erp-plan', 'failover');
    const name = mockK8sCreate.mock.calls[0][0].data.metadata.name;
    expect(name).toMatch(/^erp-plan-failover-\d+$/);
  });

  it('replaces underscores with hyphens in resource name', async () => {
    mockK8sCreate.mockResolvedValue({ metadata: { name: 'test' } });
    const { create } = useCreateDRExecution();
    await create('erp-plan', 'planned_migration');
    const name = mockK8sCreate.mock.calls[0][0].data.metadata.name;
    expect(name).toMatch(/^erp-plan-planned-migration-\d+$/);
  });

  it('sets soteria.io/plan-name label', async () => {
    mockK8sCreate.mockResolvedValue({ metadata: { name: 'test' } });
    const { create } = useCreateDRExecution();
    await create('erp-plan', 'failover');
    const labels = mockK8sCreate.mock.calls[0][0].data.metadata.labels;
    expect(labels['soteria.io/plan-name']).toBe('erp-plan');
  });

  it('uses correct K8s model', async () => {
    mockK8sCreate.mockResolvedValue({ metadata: { name: 'test' } });
    const { create } = useCreateDRExecution();
    await create('erp-plan', 'failover');
    const model = mockK8sCreate.mock.calls[0][0].model;
    expect(model.apiGroup).toBe('soteria.io');
    expect(model.kind).toBe('DRExecution');
    expect(model.plural).toBe('drexecutions');
    expect(model.namespaced).toBe(false);
  });

  it('sets planName in spec', async () => {
    mockK8sCreate.mockResolvedValue({ metadata: { name: 'test' } });
    const { create } = useCreateDRExecution();
    await create('erp-plan', 'failover');
    expect(mockK8sCreate.mock.calls[0][0].data.spec.planName).toBe('erp-plan');
  });

  it('sets isCreating true then false on success', async () => {
    mockK8sCreate.mockResolvedValue({ metadata: { name: 'test' } });
    const { create } = useCreateDRExecution();
    await create('erp-plan', 'failover');
    expect(setIsCreating).toHaveBeenCalledWith(true);
    expect(setIsCreating).toHaveBeenCalledWith(false);
  });

  it('sets error on failure', async () => {
    mockK8sCreate.mockRejectedValue(new Error('concurrent execution already active'));
    const { create } = useCreateDRExecution();
    await expect(create('erp-plan', 'failover')).rejects.toThrow(
      'concurrent execution already active',
    );
    expect(setError).toHaveBeenCalledWith('concurrent execution already active');
    expect(setIsCreating).toHaveBeenCalledWith(false);
  });

  it('throws for unknown action', async () => {
    const { create } = useCreateDRExecution();
    await expect(create('erp-plan', 'unknown')).rejects.toThrow('Unknown action: unknown');
    expect(mockK8sCreate).not.toHaveBeenCalled();
  });

  it('clears error before new creation attempt', async () => {
    mockK8sCreate.mockResolvedValue({ metadata: { name: 'test' } });
    const { create } = useCreateDRExecution();
    await create('erp-plan', 'failover');
    expect(setError).toHaveBeenCalledWith(undefined);
  });

  it('exposes clearError that resets error state', () => {
    const { clearError } = useCreateDRExecution();
    clearError();
    expect(setError).toHaveBeenCalledWith(undefined);
  });

  it('resolves title-case label to correct action key', async () => {
    mockK8sCreate.mockResolvedValue({ metadata: { name: 'test' } });
    const { create } = useCreateDRExecution();
    await create('erp-plan', 'Failover');
    expect(mockK8sCreate.mock.calls[0][0].data.spec.mode).toBe('disaster');
    expect(mockK8sCreate.mock.calls[0][0].data.metadata.name).toMatch(/^erp-plan-failover-\d+$/);
  });
});

describe('resolveActionKey', () => {
  it('returns lowercase key unchanged', () => {
    expect(resolveActionKey('failover')).toBe('failover');
    expect(resolveActionKey('planned_migration')).toBe('planned_migration');
  });

  it('resolves title-case label to lowercase key', () => {
    expect(resolveActionKey('Failover')).toBe('failover');
    expect(resolveActionKey('Reprotect')).toBe('reprotect');
    expect(resolveActionKey('Restore')).toBe('restore');
    expect(resolveActionKey('Failback')).toBe('failback');
  });

  it('resolves "Planned Migration" label to planned_migration key', () => {
    expect(resolveActionKey('Planned Migration')).toMatch(/^planned_(migration|failback)$/);
  });

  it('passes through unknown strings unchanged', () => {
    expect(resolveActionKey('bogus')).toBe('bogus');
  });
});

describe('ACTION_CONFIG completeness', () => {
  it('has config for all expected actions', () => {
    const expected = ['failover', 'planned_migration', 'reprotect', 'failback', 'planned_failback', 'restore'];
    expected.forEach((action) => {
      expect(ACTION_CONFIG[action]).toBeDefined();
      expect(ACTION_CONFIG[action].mode).toBeDefined();
      expect(ACTION_CONFIG[action].keyword).toBeDefined();
      expect(ACTION_CONFIG[action].label).toBeDefined();
      expect(ACTION_CONFIG[action].confirmVariant).toMatch(/^(danger|primary)$/);
    });
  });

  it('only failover uses danger confirmVariant', () => {
    expect(ACTION_CONFIG['failover'].confirmVariant).toBe('danger');
    ['planned_migration', 'reprotect', 'failback', 'planned_failback', 'restore'].forEach((action) => {
      expect(ACTION_CONFIG[action].confirmVariant).toBe('primary');
    });
  });
});
