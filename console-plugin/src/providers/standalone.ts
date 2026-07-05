import { useState, useEffect, useRef } from 'react';
import type { K8sProvider, K8sGroupVersionKind, WatchOpts, K8sModel } from './types';

function resolvePlural(opts: WatchOpts | undefined, kind: string): string {
  if (opts?.plural) return opts.plural;
  return kind.toLowerCase() + 's';
}

function buildApiPath(group: string, version: string, plural: string, name?: string): string {
  const base = group
    ? `/api/k8s/apis/${group}/${version}/${plural}`
    : `/api/k8s/api/${version}/${plural}`;
  return name ? `${base}/${name}` : base;
}

function buildModelPath(model: K8sModel, name?: string): string {
  return buildApiPath(model.apiGroup, model.apiVersion, model.plural, name);
}

function buildLabelSelector(matchLabels?: Record<string, string>): string {
  if (!matchLabels) return '';
  return Object.entries(matchLabels)
    .map(([k, v]) => `${k}=${v}`)
    .join(',');
}

interface WatchEvent<T> {
  type: 'ADDED' | 'MODIFIED' | 'DELETED' | 'ERROR';
  object: T;
}

function useWatchResource<T>(
  gvk: K8sGroupVersionKind | null,
  opts?: WatchOpts,
): [T, boolean, unknown] {
  const isList = opts?.isList ?? false;
  const emptyVal = (isList ? [] : undefined) as unknown as T;

  const [data, setData] = useState<T>(emptyVal);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const mountedRef = useRef(true);
  const resourceVersionRef = useRef<string>('');
  const itemsMapRef = useRef<Map<string, unknown>>(new Map());

  const group = gvk?.group ?? '';
  const version = gvk?.version ?? '';
  const kind = gvk?.kind ?? '';
  const plural = resolvePlural(opts, kind);
  const name = opts?.name;
  const selectorStr = buildLabelSelector(opts?.selector?.matchLabels);

  useEffect(() => {
    mountedRef.current = true;

    // Reset state when target changes
    setData(emptyVal);
    setLoaded(false);
    setError(null);
    resourceVersionRef.current = '';
    itemsMapRef.current = new Map();

    if (!kind) {
      setLoaded(true);
      return;
    }

    const controller = new AbortController();
    let url = buildApiPath(group, version, plural, name);
    const params = new URLSearchParams();
    if (selectorStr) params.set('labelSelector', selectorStr);

    async function initialFetch(): Promise<boolean> {
      try {
        const fetchUrl = params.toString() ? `${url}?${params}` : url;
        const resp = await fetch(fetchUrl, { signal: controller.signal });
        if (!resp.ok) throw new Error(`HTTP ${resp.status}: ${resp.statusText}`);
        const json = await resp.json();
        if (!mountedRef.current) return false;

        if (isList) {
          resourceVersionRef.current = json.metadata?.resourceVersion ?? '';
          const items = json.items ?? [];
          itemsMapRef.current = new Map(
            items.map((item: { metadata?: { name?: string } }) => [
              item.metadata?.name ?? '',
              item,
            ]),
          );
          setData(items as unknown as T);
        } else {
          resourceVersionRef.current = json.metadata?.resourceVersion ?? '';
          setData(json as T);
        }
        setLoaded(true);
        setError(null);
        return true;
      } catch (e) {
        if ((e as Error).name === 'AbortError') return false;
        if (!mountedRef.current) return false;
        setError(e);
        setLoaded(true);
        return false;
      }
    }

    async function startWatch(backoffMs = 1000) {
      const watchParams = new URLSearchParams(params);
      watchParams.set('watch', 'true');
      if (resourceVersionRef.current) {
        watchParams.set('resourceVersion', resourceVersionRef.current);
      }

      const watchUrl = `${url}?${watchParams}`;
      let receivedData = false;
      try {
        const resp = await fetch(watchUrl, { signal: controller.signal });
        if (!resp.ok || !resp.body) {
          throw new Error(`Watch failed: HTTP ${resp.status}`);
        }

        const reader = resp.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        while (true) {
          const { done, value } = await reader.read();
          if (done || !mountedRef.current) break;

          receivedData = true;
          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split('\n');
          buffer = lines.pop() ?? '';

          for (const line of lines) {
            if (!line.trim()) continue;
            try {
              const event: WatchEvent<{ metadata?: { name?: string; resourceVersion?: string } }> =
                JSON.parse(line);

              if (event.type === 'ERROR') {
                throw new Error('Watch stream error event');
              }

              const objName = event.object?.metadata?.name ?? '';
              const rv = event.object?.metadata?.resourceVersion;
              if (rv) resourceVersionRef.current = rv;

              if (isList) {
                if (event.type === 'DELETED') {
                  itemsMapRef.current.delete(objName);
                } else {
                  itemsMapRef.current.set(objName, event.object);
                }
                setData(Array.from(itemsMapRef.current.values()) as unknown as T);
              } else {
                if (event.type === 'DELETED') {
                  setData(emptyVal);
                } else {
                  setData(event.object as unknown as T);
                }
              }
            } catch {
              // Skip malformed line; stream continues
            }
          }
        }
      } catch (e) {
        if ((e as Error).name === 'AbortError') return;
        if (!mountedRef.current) return;
      }

      if (!mountedRef.current || controller.signal.aborted) return;

      // Exponential backoff with jitter; reset on successful data receipt
      const nextBackoff = receivedData
        ? 1000
        : Math.min(backoffMs * 2, 30000);
      const jitter = Math.random() * 500;

      await new Promise((resolve) => setTimeout(resolve, nextBackoff + jitter));
      if (!mountedRef.current || controller.signal.aborted) return;

      const ok = await initialFetch();
      if (ok && mountedRef.current && !controller.signal.aborted) {
        startWatch(nextBackoff);
      }
    }

    (async () => {
      const ok = await initialFetch();
      if (ok && mountedRef.current && !controller.signal.aborted) {
        startWatch();
      }
    })();

    return () => {
      mountedRef.current = false;
      controller.abort();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [group, version, kind, plural, name, isList, selectorStr]);

  return [data, loaded, error];
}

async function createResource<T>(model: K8sModel, data: object): Promise<T> {
  const url = buildModelPath(model);
  const resp = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  });
  if (!resp.ok) {
    const body = await resp.text();
    throw new Error(body || `HTTP ${resp.status}: ${resp.statusText}`);
  }
  return resp.json();
}

async function patchResource<T>(
  model: K8sModel,
  resource: { metadata: { name: string } },
  patches: object[],
): Promise<T> {
  const url = buildModelPath(model, resource.metadata.name);
  const resp = await fetch(url, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json-patch+json' },
    body: JSON.stringify(patches),
  });
  if (!resp.ok) {
    const body = await resp.text();
    throw new Error(body || `HTTP ${resp.status}: ${resp.statusText}`);
  }
  return resp.json();
}

const StandaloneDocumentTitle: React.FC<{ children: string }> = ({ children }) => {
  useEffect(() => {
    document.title = children;
  }, [children]);
  return null;
};

export const standaloneProvider: K8sProvider = {
  useWatchResource,
  createResource,
  patchResource,
  DocumentTitle: StandaloneDocumentTitle,
};
