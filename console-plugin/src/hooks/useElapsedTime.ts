import { useEffect, useRef, useState } from 'react';

export interface UseElapsedTimeResult {
  elapsed: string;
  elapsedMs: number;
}

export function formatElapsedMs(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '0s';
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  if (minutes < 60) return `${minutes}m ${remainingSeconds}s`;
  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  return `${hours}h ${remainingMinutes}m`;
}

export function useElapsedTime(
  startTime: string | undefined,
  isRunning: boolean,
): UseElapsedTimeResult {
  const [elapsedMs, setElapsedMs] = useState(0);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    if (intervalRef.current) {
      clearInterval(intervalRef.current);
      intervalRef.current = null;
    }

    if (!startTime) {
      setElapsedMs(0);
      return;
    }

    function update() {
      setElapsedMs(Date.now() - new Date(startTime!).getTime());
    }

    update();

    if (isRunning) {
      intervalRef.current = setInterval(update, 1000);
    }

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [startTime, isRunning]);

  return {
    elapsed: startTime ? formatElapsedMs(elapsedMs) : '0s',
    elapsedMs,
  };
}
