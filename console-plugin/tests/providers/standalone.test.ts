import { standaloneProvider } from '../../src/providers/standalone';

const fetchMock = jest.fn();
global.fetch = fetchMock;

jest.useFakeTimers();

let pendingCleanups: Array<() => void> = [];

jest.mock('react', () => {
  const actualReact = jest.requireActual('react');
  let stateStore: unknown[] = [];
  let stateIndex = 0;
  let effectCallbacks: Array<() => void | (() => void)> = [];

  return {
    ...actualReact,
    useState: jest.fn((init: unknown) => {
      const idx = stateIndex++;
      if (stateStore[idx] === undefined) stateStore[idx] = init;
      const setter = (val: unknown) => {
        stateStore[idx] = typeof val === 'function' ? (val as (prev: unknown) => unknown)(stateStore[idx]) : val;
      };
      return [stateStore[idx], setter];
    }),
    useEffect: jest.fn((cb: () => void | (() => void), _deps?: unknown[]) => {
      effectCallbacks.push(cb);
    }),
    useRef: jest.fn((init: unknown) => ({ current: init })),
    useCallback: jest.fn((fn: unknown) => fn),
    __resetHooks: () => {
      stateStore = [];
      stateIndex = 0;
      effectCallbacks = [];
    },
    __flushEffects: () => {
      const cleanups: Array<() => void> = [];
      effectCallbacks.forEach((cb) => {
        const cleanup = cb();
        if (typeof cleanup === 'function') cleanups.push(cleanup);
      });
      effectCallbacks = [];
      return cleanups;
    },
    __getState: () => stateStore,
    __resetStateIndex: () => {
      stateIndex = 0;
    },
  };
});

const { __resetHooks, __resetStateIndex, __flushEffects, __getState } = jest.requireMock('react');

beforeEach(() => {
  pendingCleanups.forEach((fn) => fn());
  pendingCleanups = [];
  fetchMock.mockReset();
  __resetHooks();
});

function flushAndTrackCleanups(): void {
  const cleanups = __flushEffects() as Array<() => void>;
  pendingCleanups.push(...cleanups);
}

describe('standaloneProvider.createResource', () => {
  const model = { apiGroup: 'soteria.io', apiVersion: 'v1alpha1', kind: 'DRExecution', plural: 'drexecutions', namespaced: false };

  it('sends POST to correct API path', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: () => Promise.resolve({ metadata: { name: 'test' } }) });
    await standaloneProvider.createResource(model, { spec: {} });
    expect(fetchMock).toHaveBeenCalledWith('/api/k8s/apis/soteria.io/v1alpha1/drexecutions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ spec: {} }),
    });
  });

  it('uses core API path when apiGroup is empty', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: () => Promise.resolve({ metadata: { name: 'test' } }) });
    const coreModel = { apiGroup: '', apiVersion: 'v1', kind: 'Pod', plural: 'pods', namespaced: true };
    await standaloneProvider.createResource(coreModel, { spec: {} });
    expect(fetchMock.mock.calls[0][0]).toBe('/api/k8s/api/v1/pods');
  });

  it('returns parsed JSON on success', async () => {
    const expected = { metadata: { name: 'exec-1' } };
    fetchMock.mockResolvedValue({ ok: true, json: () => Promise.resolve(expected) });
    const result = await standaloneProvider.createResource(model, { spec: {} });
    expect(result).toEqual(expected);
  });

  it('throws on HTTP error with body text', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 409, statusText: 'Conflict', text: () => Promise.resolve('already exists') });
    await expect(standaloneProvider.createResource(model, { spec: {} })).rejects.toThrow('already exists');
  });

  it('throws with status text when body is empty', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500, statusText: 'Internal Server Error', text: () => Promise.resolve('') });
    await expect(standaloneProvider.createResource(model, { spec: {} })).rejects.toThrow('HTTP 500: Internal Server Error');
  });
});

describe('standaloneProvider.patchResource', () => {
  const model = { apiGroup: 'soteria.io', apiVersion: 'v1alpha1', kind: 'DRExecution', plural: 'drexecutions', namespaced: false };

  it('sends PATCH with json-patch+json content type', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: () => Promise.resolve({}) });
    const patches = [{ op: 'replace', path: '/spec/retry', value: true }];
    await standaloneProvider.patchResource(model, { metadata: { name: 'exec-1' } }, patches);
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/k8s/apis/soteria.io/v1alpha1/drexecutions/exec-1',
      {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json-patch+json' },
        body: JSON.stringify(patches),
      },
    );
  });

  it('includes resource name in URL', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: () => Promise.resolve({}) });
    await standaloneProvider.patchResource(model, { metadata: { name: 'my-exec' } }, []);
    expect(fetchMock.mock.calls[0][0]).toContain('/my-exec');
  });

  it('throws on HTTP error', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 404, statusText: 'Not Found', text: () => Promise.resolve('resource not found') });
    await expect(
      standaloneProvider.patchResource(model, { metadata: { name: 'gone' } }, []),
    ).rejects.toThrow('resource not found');
  });
});

describe('standaloneProvider.useWatchResource', () => {
  const gvk = { group: 'soteria.io', version: 'v1alpha1', kind: 'DRPlan' };

  function mockFetchForInitialList(items: object[] = []) {
    fetchMock.mockImplementation((url: string) => {
      if (url.includes('watch=true')) {
        // Watch request: return a stream that closes immediately
        const stream = new ReadableStream({ start(ctrl) { ctrl.close(); } });
        return Promise.resolve({ ok: true, body: stream });
      }
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ metadata: { resourceVersion: '100' }, items }),
      });
    });
  }

  function mockFetchForSingle(obj: object) {
    fetchMock.mockImplementation((url: string) => {
      if (url.includes('watch=true')) {
        const stream = new ReadableStream({ start(ctrl) { ctrl.close(); } });
        return Promise.resolve({ ok: true, body: stream });
      }
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve(obj),
      });
    });
  }

  it('uses opts.plural for URL construction when provided', async () => {
    mockFetchForInitialList();

    __resetStateIndex();
    standaloneProvider.useWatchResource(gvk, { isList: true, plural: 'drplans' });
    flushAndTrackCleanups();
    await Promise.resolve();

    const listCalls = fetchMock.mock.calls.filter(
      (c: [string]) => !c[0].includes('watch=true'),
    );
    expect(listCalls[0][0]).toBe('/api/k8s/apis/soteria.io/v1alpha1/drplans');
  });

  it('falls back to kind+s pluralization when opts.plural is not provided', async () => {
    mockFetchForInitialList();

    __resetStateIndex();
    standaloneProvider.useWatchResource(gvk, { isList: true });
    flushAndTrackCleanups();
    await Promise.resolve();

    const listCalls = fetchMock.mock.calls.filter(
      (c: [string]) => !c[0].includes('watch=true'),
    );
    expect(listCalls[0][0]).toBe('/api/k8s/apis/soteria.io/v1alpha1/drplans');
  });

  it('includes labelSelector query param when selector is provided', async () => {
    mockFetchForInitialList();

    __resetStateIndex();
    standaloneProvider.useWatchResource(gvk, {
      isList: true,
      plural: 'drplans',
      selector: { matchLabels: { 'soteria.io/plan-name': 'erp' } },
    });
    flushAndTrackCleanups();
    await Promise.resolve();

    const listCalls = fetchMock.mock.calls.filter(
      (c: [string]) => !c[0].includes('watch=true'),
    );
    expect(listCalls[0][0]).toContain('labelSelector=');
    expect(listCalls[0][0]).toContain('soteria.io%2Fplan-name%3Derp');
  });

  it('resets loaded to false and data to empty when effect runs', () => {
    __resetStateIndex();
    standaloneProvider.useWatchResource(gvk, { isList: true, plural: 'drplans' });
    flushAndTrackCleanups();

    const state = __getState();
    expect(state[0]).toEqual([]);
    expect(state[1]).toBe(false);
  });

  it('returns loaded=true with empty data when gvk is null', () => {
    __resetStateIndex();
    standaloneProvider.useWatchResource(null, { isList: true });
    flushAndTrackCleanups();

    const state = __getState();
    expect(state[0]).toEqual([]);
    expect(state[1]).toBe(true);
    expect(state[2]).toBeNull();
  });

  it('sets error and loaded=true on fetch failure', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 403,
      statusText: 'Forbidden',
    });

    __resetStateIndex();
    standaloneProvider.useWatchResource(gvk, { isList: true, plural: 'drplans' });
    flushAndTrackCleanups();
    await Promise.resolve();
    await Promise.resolve();

    const state = __getState();
    expect(state[1]).toBe(true);
    expect(state[2]).toBeInstanceOf(Error);
    expect((state[2] as Error).message).toContain('403');
  });

  it('fetches single resource URL when name is provided', async () => {
    mockFetchForSingle({ metadata: { name: 'my-plan', resourceVersion: '5' } });

    __resetStateIndex();
    standaloneProvider.useWatchResource(gvk, { name: 'my-plan', isList: false, plural: 'drplans' });
    flushAndTrackCleanups();
    await Promise.resolve();

    const getCalls = fetchMock.mock.calls.filter(
      (c: [string]) => !c[0].includes('watch=true'),
    );
    expect(getCalls[0][0]).toBe('/api/k8s/apis/soteria.io/v1alpha1/drplans/my-plan');
  });

  it('returns undefined for singular watch initial state', () => {
    __resetStateIndex();
    standaloneProvider.useWatchResource(gvk, { name: 'my-plan', isList: false, plural: 'drplans' });

    const state = __getState();
    expect(state[0]).toBeUndefined();
  });
});
