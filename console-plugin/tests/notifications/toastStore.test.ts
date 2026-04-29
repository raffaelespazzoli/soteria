import {
  addToast,
  removeToast,
  getSnapshot,
  subscribe,
  resetForTesting,
} from '../../src/notifications/toastStore';

beforeEach(() => {
  resetForTesting();
  jest.useFakeTimers();
});

afterEach(() => {
  jest.useRealTimers();
});

describe('toastStore', () => {
  it('adds a toast and notifies subscribers', () => {
    const listener = jest.fn();
    subscribe(listener);
    addToast({ variant: 'info', title: 'Test toast', persistent: false, timeout: 8000 });
    expect(listener).toHaveBeenCalled();
    expect(getSnapshot()).toHaveLength(1);
    expect(getSnapshot()[0]).toMatchObject({ variant: 'info', title: 'Test toast' });
  });

  it('generates unique IDs for each toast', () => {
    addToast({ variant: 'info', title: 'Toast 1', persistent: false, timeout: 8000 });
    addToast({ variant: 'info', title: 'Toast 2', persistent: false, timeout: 8000 });
    const [first, second] = getSnapshot();
    expect(first.id).not.toBe(second.id);
  });

  it('removes a toast and notifies subscribers', () => {
    addToast({ variant: 'info', title: 'Test', persistent: false, timeout: 8000 });
    const [toast] = getSnapshot();
    const listener = jest.fn();
    subscribe(listener);
    removeToast(toast.id);
    expect(listener).toHaveBeenCalled();
    expect(getSnapshot()).toHaveLength(0);
  });

  it('auto-dismisses non-persistent toast after timeout', () => {
    addToast({ variant: 'info', title: 'Auto dismiss', persistent: false, timeout: 8000 });
    expect(getSnapshot()).toHaveLength(1);
    jest.advanceTimersByTime(8000);
    expect(getSnapshot()).toHaveLength(0);
  });

  it('does not auto-dismiss persistent toast', () => {
    addToast({ variant: 'warning', title: 'Persist', persistent: true, timeout: 0 });
    jest.advanceTimersByTime(60000);
    expect(getSnapshot()).toHaveLength(1);
  });

  it('evicts oldest non-persistent toast when exceeding max (8)', () => {
    for (let i = 0; i < 8; i++) {
      addToast({ variant: 'info', title: `Toast ${i}`, persistent: false, timeout: 30000 });
    }
    expect(getSnapshot()).toHaveLength(8);

    addToast({ variant: 'success', title: 'Toast overflow', persistent: false, timeout: 30000 });
    const toasts = getSnapshot();
    expect(toasts).toHaveLength(8);
    expect(toasts[0].title).toBe('Toast 1');
    expect(toasts[7].title).toBe('Toast overflow');
  });

  it('subscribe returns an unsubscribe function', () => {
    const listener = jest.fn();
    const unsub = subscribe(listener);
    addToast({ variant: 'info', title: 'Before unsub', persistent: false, timeout: 8000 });
    expect(listener).toHaveBeenCalledTimes(1);

    unsub();
    addToast({ variant: 'info', title: 'After unsub', persistent: false, timeout: 8000 });
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it('resetForTesting clears all toasts and listeners', () => {
    const listener = jest.fn();
    subscribe(listener);
    addToast({ variant: 'info', title: 'Will clear', persistent: false, timeout: 8000 });

    resetForTesting();

    expect(getSnapshot()).toHaveLength(0);
    addToast({ variant: 'info', title: 'After reset', persistent: false, timeout: 8000 });
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it('removing a non-existent toast is a no-op', () => {
    addToast({ variant: 'info', title: 'Exists', persistent: false, timeout: 8000 });
    removeToast('non-existent-id');
    expect(getSnapshot()).toHaveLength(1);
  });

  it('clearing timer on manual remove prevents auto-dismiss callback', () => {
    addToast({ variant: 'info', title: 'Manual remove', persistent: false, timeout: 5000 });
    const [toast] = getSnapshot();
    removeToast(toast.id);
    expect(getSnapshot()).toHaveLength(0);
    jest.advanceTimersByTime(5000);
    expect(getSnapshot()).toHaveLength(0);
  });
});
