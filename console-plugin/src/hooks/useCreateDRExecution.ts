import { useState, useCallback } from 'react';
import { k8sCreate } from '@openshift-console/dynamic-plugin-sdk';
import { DRExecution } from '../models/types';
import { ACTION_CONFIG, resolveActionKey } from '../utils/drPlanActions';

const drExecutionModel = {
  apiGroup: 'soteria.io',
  apiVersion: 'v1alpha1',
  kind: 'DRExecution',
  abbr: 'DRE',
  label: 'DRExecution',
  labelPlural: 'DRExecutions',
  plural: 'drexecutions',
  namespaced: false,
};

export function useCreateDRExecution(): {
  create: (planName: string, action: string) => Promise<DRExecution>;
  isCreating: boolean;
  error: string | undefined;
  clearError: () => void;
} {
  const [isCreating, setIsCreating] = useState(false);
  const [error, setError] = useState<string | undefined>();

  const clearError = useCallback(() => setError(undefined), []);

  const create = useCallback(async (planName: string, action: string): Promise<DRExecution> => {
    const key = resolveActionKey(action);
    const config = ACTION_CONFIG[key];
    if (!config) throw new Error(`Unknown action: ${action}`);

    setIsCreating(true);
    setError(undefined);

    try {
      const result = await k8sCreate({
        model: drExecutionModel,
        data: {
          apiVersion: 'soteria.io/v1alpha1',
          kind: 'DRExecution',
          metadata: {
            name: `${planName}-${key.replace(/_/g, '-')}-${Date.now()}`,
            labels: { 'soteria.io/plan-name': planName },
          },
          spec: {
            planName,
            mode: config.mode,
          },
        },
      });
      setIsCreating(false);
      return result as DRExecution;
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setError(message);
      setIsCreating(false);
      throw e;
    }
  }, []);

  return { create, isCreating, error, clearError };
}
