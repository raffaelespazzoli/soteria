import { useEffect, useReducer } from 'react';
import { Toast, getSnapshot, subscribe, removeToast } from '../notifications/toastStore';

export interface UseToastNotificationsResult {
  toasts: Toast[];
  removeToast: (id: string) => void;
}

export function useToastNotifications(): UseToastNotificationsResult {
  const [, forceUpdate] = useReducer((x: number) => x + 1, 0);

  useEffect(() => {
    const unsubscribe = subscribe(forceUpdate);
    return unsubscribe;
  }, []);

  return { toasts: getSnapshot(), removeToast };
}
