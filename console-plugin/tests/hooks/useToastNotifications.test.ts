import { render, screen, act } from '@testing-library/react';
import { createElement } from 'react';
import { useToastNotifications } from '../../src/hooks/useToastNotifications';
import { addToast, resetForTesting } from '../../src/notifications/toastStore';

beforeEach(() => {
  resetForTesting();
  jest.useFakeTimers();
});

afterEach(() => {
  jest.useRealTimers();
});

const HookOutput: React.FC = () => {
  const { toasts, removeToast } = useToastNotifications();
  return createElement(
    'div',
    null,
    createElement('span', { 'data-testid': 'count' }, String(toasts.length)),
    createElement('span', { 'data-testid': 'titles' }, toasts.map((t) => t.title).join(',')),
    toasts.map((t) =>
      createElement('button', {
        key: t.id,
        'data-testid': `remove-${t.id}`,
        onClick: () => removeToast(t.id),
      }, `Remove ${t.id}`),
    ),
  );
};

describe('useToastNotifications', () => {
  it('returns current toasts from store', () => {
    addToast({ variant: 'info', title: 'Hello', persistent: false, timeout: 8000 });
    render(createElement(HookOutput));
    expect(screen.getByTestId('count').textContent).toBe('1');
    expect(screen.getByTestId('titles').textContent).toBe('Hello');
  });

  it('updates when store changes', () => {
    render(createElement(HookOutput));
    expect(screen.getByTestId('count').textContent).toBe('0');

    act(() => {
      addToast({ variant: 'success', title: 'Added', persistent: false, timeout: 8000 });
    });
    expect(screen.getByTestId('count').textContent).toBe('1');
  });

  it('removeToast removes from store', () => {
    addToast({ variant: 'info', title: 'Remove me', persistent: true, timeout: 0 });
    render(createElement(HookOutput));
    expect(screen.getByTestId('count').textContent).toBe('1');

    const toastId = screen.getByTestId('titles').textContent;
    expect(toastId).toBe('Remove me');

    const buttons = screen.getAllByRole('button');
    act(() => {
      buttons[0].click();
    });
    expect(screen.getByTestId('count').textContent).toBe('0');
  });

  it('cleans up subscription on unmount', () => {
    const { unmount } = render(createElement(HookOutput));
    expect(screen.getByTestId('count').textContent).toBe('0');
    unmount();

    act(() => {
      addToast({ variant: 'info', title: 'After unmount', persistent: false, timeout: 8000 });
    });
  });

  it('reflects auto-dismiss after timeout', () => {
    render(createElement(HookOutput));

    act(() => {
      addToast({ variant: 'info', title: 'Auto-dismiss', persistent: false, timeout: 5000 });
    });
    expect(screen.getByTestId('count').textContent).toBe('1');

    act(() => {
      jest.advanceTimersByTime(5000);
    });
    expect(screen.getByTestId('count').textContent).toBe('0');
  });
});
