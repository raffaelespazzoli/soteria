import {
  useK8sWatchResource,
  k8sCreate,
  k8sPatch,
  DocumentTitle,
} from '@openshift-console/dynamic-plugin-sdk';
import type { WatchK8sResource } from '@openshift-console/dynamic-plugin-sdk';
import type { K8sProvider, K8sGroupVersionKind, WatchOpts, K8sModel } from './types';

function useWatchResource<T>(
  gvk: K8sGroupVersionKind | null,
  opts?: WatchOpts,
): [T, boolean, unknown] {
  const resource: WatchK8sResource | null = gvk ? { groupVersionKind: gvk, ...opts } : null;
  return useK8sWatchResource<T>(resource);
}

async function createResource<T>(model: K8sModel, data: object): Promise<T> {
  return k8sCreate({ model, data }) as Promise<T>;
}

async function patchResource<T>(
  model: K8sModel,
  resource: { metadata: { name: string } },
  patches: object[],
): Promise<T> {
  return k8sPatch({ model, resource, data: patches }) as Promise<T>;
}

export const ocpProvider: K8sProvider = {
  useWatchResource,
  createResource,
  patchResource,
  DocumentTitle,
};
