import { useEffect, useState } from 'react';
import {
  K8sGroupVersionKind,
  useK8sWatchResource,
  WatchK8sResource,
} from '@openshift-console/dynamic-plugin-sdk';
import { DRExecution, DRGroupStatus, DRPlan } from '../models/types';

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

const drGroupStatusGVK: K8sGroupVersionKind = {
  group: 'soteria.io',
  version: 'v1alpha1',
  kind: 'DRGroupStatus',
};

export function useDRPlans(): [DRPlan[], boolean, unknown] {
  const resource: WatchK8sResource = {
    groupVersionKind: drPlanGVK,
    isList: true,
  };
  return useK8sWatchResource<DRPlan[]>(resource);
}

export function useDRPlan(name: string): [DRPlan | undefined, boolean, unknown] {
  const resource: WatchK8sResource = {
    groupVersionKind: drPlanGVK,
    name,
    isList: false,
  };
  const [data, loaded, error] = useK8sWatchResource<DRPlan>(resource);
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
  const resource: WatchK8sResource = {
    groupVersionKind: drExecutionGVK,
    isList: true,
    ...(planName ? { selector: { matchLabels: { 'soteria.io/plan-name': planName } } } : {}),
  };
  return useK8sWatchResource<DRExecution[]>(resource);
}

export function useDRExecution(name: string): [DRExecution | undefined, boolean, unknown] {
  const resource: WatchK8sResource | null = name
    ? { groupVersionKind: drExecutionGVK, name, isList: false }
    : null;
  const [data, loaded, error] = useK8sWatchResource<DRExecution>(resource);
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

export function useDRGroupStatuses(executionName?: string): [DRGroupStatus[], boolean, unknown] {
  const resource: WatchK8sResource = {
    groupVersionKind: drGroupStatusGVK,
    isList: true,
  };
  const [data, loaded, error] = useK8sWatchResource<DRGroupStatus[]>(resource);
  const filtered =
    executionName && loaded && !error
      ? data.filter((gs) => gs.spec?.executionName === executionName)
      : data;
  return [filtered, loaded, error];
}
