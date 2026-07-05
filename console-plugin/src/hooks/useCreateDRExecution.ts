import { useState, useCallback } from 'react';
import { useProvider } from '../providers';
import { DRExecution } from '../models/types';
import { drExecutionModel } from '../models/k8sModels';
import { ACTION_CONFIG, resolveActionKey } from '../utils/drPlanActions';

export function useCreateDRExecution(): {
  create: (planName: string, action: string) => Promise<DRExecution>;
  isCreating: boolean;
  error: string | undefined;
  clearError: () => void;
} {
  const { createResource } = useProvider();
  const [isCreating, setIsCreating] = useState(false);
  const [error, setError] = useState<string | undefined>();

  const clearError = useCallback(() => setError(undefined), []);

  const create = useCallback(
    async (planName: string, action: string): Promise<DRExecution> => {
      const key = resolveActionKey(action);
      const config = ACTION_CONFIG[key];
      if (!config) throw new Error(`Unknown action: ${action}`);

      setIsCreating(true);
      setError(undefined);

      try {
        const result = await createResource<DRExecution>(drExecutionModel, {
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
        });
        setIsCreating(false);
        return result;
      } catch (e) {
        const message = e instanceof Error ? e.message : String(e);
        setError(message);
        setIsCreating(false);
        throw e;
      }
    },
    [createResource],
  );

  return { create, isCreating, error, clearError };
}
