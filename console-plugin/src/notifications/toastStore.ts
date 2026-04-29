export interface Toast {
  id: string;
  variant: 'info' | 'success' | 'warning' | 'danger';
  title: string;
  description?: string;
  linkText?: string;
  linkTo?: string;
  persistent: boolean;
  timeout: number;
}

type Listener = () => void;

const MAX_TOASTS = 8;

let toasts: Toast[] = [];
const listeners = new Set<Listener>();
const timers = new Map<string, ReturnType<typeof setTimeout>>();

function notify(): void {
  listeners.forEach((l) => l());
}

export function getSnapshot(): Toast[] {
  return toasts;
}

export function subscribe(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function addToast(toast: Omit<Toast, 'id'>): void {
  const id = `toast-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  const entry: Toast = { ...toast, id };
  toasts = [...toasts, entry];
  if (toasts.length > MAX_TOASTS) {
    const evicted = toasts.find((t) => !t.persistent);
    if (evicted) {
      toasts = toasts.filter((t) => t.id !== evicted.id);
      clearTimer(evicted.id);
    } else {
      toasts = toasts.slice(-MAX_TOASTS);
    }
  }
  notify();
  if (!toast.persistent && toast.timeout > 0) {
    timers.set(
      id,
      setTimeout(() => removeToast(id), toast.timeout),
    );
  }
}

export function removeToast(id: string): void {
  toasts = toasts.filter((t) => t.id !== id);
  clearTimer(id);
  notify();
}

function clearTimer(id: string): void {
  const timer = timers.get(id);
  if (timer) {
    clearTimeout(timer);
    timers.delete(id);
  }
}

export function resetForTesting(): void {
  toasts = [];
  timers.forEach((timer) => clearTimeout(timer));
  timers.clear();
  listeners.clear();
}
