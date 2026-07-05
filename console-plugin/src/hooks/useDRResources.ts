import { useEffect, useState } from 'react';
import type { K8sGroupVersionKind } from '@openshift-console/dynamic-plugin-sdk';
import { useProvider } from '../providers';
import { DRExecution, DRPlan } from '../models/types';

const drPlanGVK: K8sGroupVersionKind = {
  group: 'soteria.io',
  version: 'v1alpha1',
  kind: 'DRPlan',
};

const drExecutionGVK: K8sGroupVersionKind = {
  group: 'soteria.io',
  version: 'v1alpha1',
  kind: 'DRExecution',
};

export function useDRPlans(): [DRPlan[], boolean, unknown] {
  const { useWatchResource } = useProvider();
  return useWatchResource<DRPlan[]>(drPlanGVK, { isList: true, plural: 'drplans' });
}

export function useDRPlan(name: string): [DRPlan | undefined, boolean, unknown] {
  const { useWatchResource } = useProvider();
  const [data, loaded, error] = useWatchResource<DRPlan>(drPlanGVK, { name, isList: false, plural: 'drplans' });
  const [cached, setCached] = useState<DRPlan | undefined>(undefined);

  const dataHasContent = !!(data && data.metadata?.name);

  useEffect(() => {
    if (loaded && !error && dataHasContent) {
      setCached(data); // eslint-disable-line react-hooks/set-state-in-effect -- sync valid data to cache
    }
  }, [loaded, error, dataHasContent, data]);

  if (cached) {
    return [loaded && !error && dataHasContent ? data : cached, true, null];
  }

  return [loaded && !error ? data : undefined, loaded, error];
}

export function useDRExecutions(planName?: string): [DRExecution[], boolean, unknown] {
  const { useWatchResource } = useProvider();
  return useWatchResource<DRExecution[]>(drExecutionGVK, {
    isList: true,
    plural: 'drexecutions',
    ...(planName ? { selector: { matchLabels: { 'soteria.io/plan-name': planName } } } : {}),
  });
}

export function useDRExecution(name: string): [DRExecution | undefined, boolean, unknown] {
  const { useWatchResource } = useProvider();
  const gvk = name ? drExecutionGVK : null;
  const [data, loaded, error] = useWatchResource<DRExecution>(gvk, { name, isList: false, plural: 'drexecutions' });
  const [cached, setCached] = useState<DRExecution | undefined>(undefined);

  const dataHasContent = !!(data && data.metadata?.name);

  useEffect(() => {
    if (!name) {
      setCached(undefined); // eslint-disable-line react-hooks/set-state-in-effect -- clear cache on name change
    } else if (loaded && !error && dataHasContent) {
      setCached(data);
    }
  }, [name, loaded, error, dataHasContent, data]);

  if (!name) {
    return [undefined, true, null];
  }

  if (cached) {
    return [loaded && !error && dataHasContent ? data : cached, true, null];
  }

  return [loaded && !error ? data : undefined, loaded, error];
}
