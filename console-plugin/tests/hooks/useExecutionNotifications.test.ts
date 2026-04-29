import { createElement } from 'react';
import { render } from '@testing-library/react';
import { DRExecution } from '../../src/models/types';
import * as toastStore from '../../src/notifications/toastStore';

const mockUseDRExecutions = jest.fn<[DRExecution[], boolean, unknown], []>();

jest.mock('../../src/hooks/useDRResources', () => ({
  useDRExecutions: () => mockUseDRExecutions(),
}));

import { useExecutionNotifications } from '../../src/hooks/useExecutionNotifications';

const now = Date.now();

const mockStartedExecution: DRExecution = {
  apiVersion: 'soteria.io/v1alpha1',
  kind: 'DRExecution',
  metadata: {
    name: 'erp-failover-001',
    uid: '1',
    labels: { 'soteria.io/plan-name': 'erp-full-stack' },
  },
  spec: { planName: 'erp-full-stack', mode: 'disaster' },
  status: {
    startTime: new Date(now).toISOString(),
    waves: [],
  },
};

const mockSucceededExecution: DRExecution = {
  ...mockStartedExecution,
  status: {
    result: 'Succeeded',
    startTime: new Date(now - 17 * 60 * 1000).toISOString(),
    completionTime: new Date(now).toISOString(),
    rpoSeconds: 47,
    waves: [
      {
        waveIndex: 0,
        groups: [
          { name: 'drgroup-1', result: 'Completed', vmNames: ['vm1', 'vm2'] },
          { name: 'drgroup-2', result: 'Completed', vmNames: ['vm3'] },
        ],
      },
      {
        waveIndex: 1,
        groups: [
          { name: 'drgroup-3', result: 'Completed', vmNames: ['vm4', 'vm5', 'vm6'] },
        ],
      },
    ],
  },
};

const mockPartialExecution: DRExecution = {
  ...mockStartedExecution,
  status: {
    result: 'PartiallySucceeded',
    startTime: new Date(now - 10 * 60 * 1000).toISOString(),
    completionTime: new Date(now).toISOString(),
    rpoSeconds: 30,
    waves: [
      {
        waveIndex: 0,
        groups: [
          { name: 'drgroup-1', result: 'Completed', vmNames: ['vm1', 'vm2'] },
          { name: 'drgroup-2', result: 'Failed', vmNames: ['vm3'], error: 'timeout' },
        ],
      },
    ],
  },
};

const mockFailedExecution: DRExecution = {
  ...mockStartedExecution,
  status: {
    result: 'Failed',
    startTime: new Date(now - 5 * 60 * 1000).toISOString(),
    completionTime: new Date(now).toISOString(),
    waves: [],
  },
};

const mockReprotectExecution: DRExecution = {
  ...mockStartedExecution,
  spec: { planName: 'erp-full-stack', mode: 'reprotect' },
  status: {
    result: 'Succeeded',
    startTime: new Date(now - 3 * 60 * 1000).toISOString(),
    completionTime: new Date(now).toISOString(),
    rpoSeconds: 0,
    waves: [],
  },
};

const mockPlannedMigration: DRExecution = {
  ...mockStartedExecution,
  spec: { planName: 'erp-full-stack', mode: 'planned_migration' },
  status: {
    startTime: new Date(now).toISOString(),
    waves: [],
  },
};

const HookHost: React.FC = () => {
  useExecutionNotifications();
  return createElement('div', { 'data-testid': 'hook-host' });
};

beforeEach(() => {
  toastStore.resetForTesting();
  jest.clearAllMocks();
});

describe('useExecutionNotifications', () => {
  it('does NOT fire toasts on initial load', () => {
    const spy = jest.spyOn(toastStore, 'addToast');
    mockUseDRExecutions.mockReturnValue([[mockStartedExecution], true, null]);
    render(createElement(HookHost));
    expect(spy).not.toHaveBeenCalled();
  });

  it('fires info toast when new execution starts', () => {
    const spy = jest.spyOn(toastStore, 'addToast');
    mockUseDRExecutions.mockReturnValue([[], true, null]);
    const { rerender } = render(createElement(HookHost));

    mockUseDRExecutions.mockReturnValue([[mockStartedExecution], true, null]);
    rerender(createElement(HookHost));

    expect(spy).toHaveBeenCalledWith(
      expect.objectContaining({
        variant: 'info',
        title: 'Failover started for erp-full-stack',
        persistent: false,
        timeout: 8000,
      }),
    );
  });

  it('fires success toast when execution succeeds', () => {
    const spy = jest.spyOn(toastStore, 'addToast');
    mockUseDRExecutions.mockReturnValue([[mockStartedExecution], true, null]);
    const { rerender } = render(createElement(HookHost));

    mockUseDRExecutions.mockReturnValue([[mockSucceededExecution], true, null]);
    rerender(createElement(HookHost));

    expect(spy).toHaveBeenCalledWith(
      expect.objectContaining({
        variant: 'success',
        persistent: false,
        timeout: 15000,
      }),
    );
    const call = spy.mock.calls[0][0];
    expect(call.title).toContain('Failover completed');
    expect(call.title).toContain('6 VMs recovered');
  });

  it('fires warning toast (persistent) when partially succeeded', () => {
    const spy = jest.spyOn(toastStore, 'addToast');
    mockUseDRExecutions.mockReturnValue([[mockStartedExecution], true, null]);
    const { rerender } = render(createElement(HookHost));

    mockUseDRExecutions.mockReturnValue([[mockPartialExecution], true, null]);
    rerender(createElement(HookHost));

    expect(spy).toHaveBeenCalledWith(
      expect.objectContaining({
        variant: 'warning',
        persistent: true,
        timeout: 0,
      }),
    );
    const call = spy.mock.calls[0][0];
    expect(call.title).toContain('partially succeeded');
    expect(call.title).toContain('1 DRGroup failed');
  });

  it('fires danger toast (persistent) when failed', () => {
    const spy = jest.spyOn(toastStore, 'addToast');
    mockUseDRExecutions.mockReturnValue([[mockStartedExecution], true, null]);
    const { rerender } = render(createElement(HookHost));

    mockUseDRExecutions.mockReturnValue([[mockFailedExecution], true, null]);
    rerender(createElement(HookHost));

    expect(spy).toHaveBeenCalledWith(
      expect.objectContaining({
        variant: 'danger',
        title: 'Failover failed for erp-full-stack',
        persistent: true,
        timeout: 0,
      }),
    );
  });

  it('fires re-protect-specific message for reprotect mode', () => {
    const reprotectStarted: DRExecution = {
      ...mockReprotectExecution,
      status: {
        startTime: new Date(now).toISOString(),
        waves: [],
      },
    };

    const spy = jest.spyOn(toastStore, 'addToast');
    mockUseDRExecutions.mockReturnValue([[reprotectStarted], true, null]);
    const { rerender } = render(createElement(HookHost));

    mockUseDRExecutions.mockReturnValue([[mockReprotectExecution], true, null]);
    rerender(createElement(HookHost));

    expect(spy).toHaveBeenCalledWith(
      expect.objectContaining({
        variant: 'success',
        title: 'Re-protect complete: replication healthy',
        persistent: false,
        timeout: 8000,
      }),
    );
  });

  it('uses correct mode labels: Failover, Planned Migration, Re-protect', () => {
    const spy = jest.spyOn(toastStore, 'addToast');
    mockUseDRExecutions.mockReturnValue([[], true, null]);
    const { rerender } = render(createElement(HookHost));

    mockUseDRExecutions.mockReturnValue([[mockPlannedMigration], true, null]);
    rerender(createElement(HookHost));

    expect(spy).toHaveBeenCalledWith(
      expect.objectContaining({
        title: 'Planned Migration started for erp-full-stack',
      }),
    );
  });

  it('includes VM count and duration in success message', () => {
    const spy = jest.spyOn(toastStore, 'addToast');
    mockUseDRExecutions.mockReturnValue([[mockStartedExecution], true, null]);
    const { rerender } = render(createElement(HookHost));

    mockUseDRExecutions.mockReturnValue([[mockSucceededExecution], true, null]);
    rerender(createElement(HookHost));

    const call = spy.mock.calls[0][0];
    expect(call.title).toMatch(/6 VMs recovered in/);
    expect(call.title).toMatch(/17m/);
  });

  it('includes failed group count in partial success message', () => {
    const spy = jest.spyOn(toastStore, 'addToast');
    mockUseDRExecutions.mockReturnValue([[mockStartedExecution], true, null]);
    const { rerender } = render(createElement(HookHost));

    mockUseDRExecutions.mockReturnValue([[mockPartialExecution], true, null]);
    rerender(createElement(HookHost));

    const call = spy.mock.calls[0][0];
    expect(call.title).toContain('1 DRGroup failed');
  });

  it('includes link to execution monitor in all toasts', () => {
    const spy = jest.spyOn(toastStore, 'addToast');
    mockUseDRExecutions.mockReturnValue([[], true, null]);
    const { rerender } = render(createElement(HookHost));

    mockUseDRExecutions.mockReturnValue([[mockStartedExecution], true, null]);
    rerender(createElement(HookHost));

    expect(spy).toHaveBeenCalledWith(
      expect.objectContaining({
        linkTo: '/disaster-recovery/executions/erp-failover-001',
        linkText: 'View execution',
      }),
    );
  });

  it('fires completed toast when execution appears already terminal', () => {
    const spy = jest.spyOn(toastStore, 'addToast');
    mockUseDRExecutions.mockReturnValue([[], true, null]);
    const { rerender } = render(createElement(HookHost));

    mockUseDRExecutions.mockReturnValue([[mockSucceededExecution], true, null]);
    rerender(createElement(HookHost));

    expect(spy).toHaveBeenCalledWith(
      expect.objectContaining({
        variant: 'success',
        linkTo: '/disaster-recovery/executions/erp-failover-001',
      }),
    );
    const call = spy.mock.calls[0][0];
    expect(call.title).toContain('Failover completed');
  });

  it('does not fire when data is not yet loaded', () => {
    const spy = jest.spyOn(toastStore, 'addToast');
    mockUseDRExecutions.mockReturnValue([[], false, null]);
    render(createElement(HookHost));
    expect(spy).not.toHaveBeenCalled();
  });
});
