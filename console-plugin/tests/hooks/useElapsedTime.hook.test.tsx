import { render, screen, act } from '@testing-library/react';
import { useElapsedTime, UseElapsedTimeResult } from '../../src/hooks/useElapsedTime';

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({}));

interface HookOutputProps {
  startTime: string | undefined;
  isRunning: boolean;
}

const HookOutput: React.FC<HookOutputProps> = ({ startTime, isRunning }) => {
  const result: UseElapsedTimeResult = useElapsedTime(startTime, isRunning);
  return (
    <div>
      <span data-testid="elapsed">{result.elapsed}</span>
      <span data-testid="elapsedMs">{result.elapsedMs}</span>
    </div>
  );
};

describe('useElapsedTime (hook behavior)', () => {
  beforeEach(() => jest.useFakeTimers());
  afterEach(() => jest.useRealTimers());

  it('returns "0s" when no startTime', () => {
    render(<HookOutput startTime={undefined} isRunning={true} />);
    expect(screen.getByTestId('elapsed').textContent).toBe('0s');
    expect(screen.getByTestId('elapsedMs').textContent).toBe('0');
  });

  it('counts up from startTime when isRunning', () => {
    const startTime = new Date(Date.now() - 10000).toISOString();
    render(<HookOutput startTime={startTime} isRunning={true} />);
    expect(screen.getByTestId('elapsed').textContent).toBe('10s');

    act(() => {
      jest.advanceTimersByTime(5000);
    });

    expect(screen.getByTestId('elapsed').textContent).toBe('15s');
  });

  it('stops counting when isRunning is false', () => {
    const startTime = new Date(Date.now() - 10000).toISOString();
    const { rerender } = render(<HookOutput startTime={startTime} isRunning={true} />);
    expect(screen.getByTestId('elapsed').textContent).toBe('10s');

    rerender(<HookOutput startTime={startTime} isRunning={false} />);

    act(() => {
      jest.advanceTimersByTime(5000);
    });

    expect(screen.getByTestId('elapsed').textContent).toBe('10s');
  });

  it('formats minutes correctly', () => {
    const startTime = new Date(Date.now() - 125000).toISOString();
    render(<HookOutput startTime={startTime} isRunning={false} />);
    expect(screen.getByTestId('elapsed').textContent).toBe('2m 5s');
  });

  it('formats hours correctly', () => {
    const startTime = new Date(Date.now() - 3725000).toISOString();
    render(<HookOutput startTime={startTime} isRunning={false} />);
    expect(screen.getByTestId('elapsed').textContent).toBe('1h 2m');
  });

  it('cleans up interval on unmount', () => {
    const clearSpy = jest.spyOn(global, 'clearInterval');
    const startTime = new Date(Date.now() - 10000).toISOString();
    const { unmount } = render(<HookOutput startTime={startTime} isRunning={true} />);
    unmount();
    expect(clearSpy).toHaveBeenCalled();
    clearSpy.mockRestore();
  });
});
