import type { K8sGroupVersionKind, K8sModel } from '@openshift-console/dynamic-plugin-sdk';

export type { K8sGroupVersionKind, K8sModel };

export interface WatchOpts {
  name?: string;
  isList?: boolean;
  plural?: string;
  selector?: { matchLabels?: Record<string, string> };
}

export interface K8sProvider {
  useWatchResource<T>(gvk: K8sGroupVersionKind | null, opts?: WatchOpts): [T, boolean, unknown];
  createResource<T>(model: K8sModel, data: object): Promise<T>;
  patchResource<T>(
    model: K8sModel,
    resource: { metadata: { name: string } },
    patches: object[],
  ): Promise<T>;
  DocumentTitle: React.FC<{ children: string }>;
}
