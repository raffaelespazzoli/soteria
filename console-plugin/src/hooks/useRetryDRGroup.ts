import { useState, useCallback, useEffect, useRef } from 'react';
import { k8sPatch } from '@openshift-console/dynamic-plugin-sdk';
import { DRExecution, Condition } from '../models/types';
import { drExecutionModel } from '../models/k8sModels';

const RETRY_GROUPS_ANNOTATION = 'soteria.io/retry-groups';
const RETRY_ALL_FAILED = 'all-failed';

export interface UseRetryDRGroupResult {
  retry: (groupName: string) => Promise<void>;
  retryAll: () => Promise<void>;
  isRetrying: boolean;
  retryError: string | null;
  retriedGroup: string | null;
}

function getRetryRejectedMessage(conditions: Condition[] | undefined): string | null {
  if (!conditions) return null;
  const condition = conditions.find(
    (c) => c.type === 'RetryRejected' && c.status === 'True',
  );
  return condition?.message ?? null;
}

export function useRetryDRGroup(
  executionName: string,
  execution: DRExecution | null | undefined,
): UseRetryDRGroupResult {
  const [isRetrying, setIsRetrying] = useState(false);
  const [retryError, setRetryError] = useState<string | null>(null);
  const [retriedGroup, setRetriedGroup] = useState<string | null>(null);
  const errorFromConditionRef = useRef(false);

  const patchAnnotation = useCallback(
    async (value: string) => {
      setIsRetrying(true);
      setRetryError(null);
      errorFromConditionRef.current = false;
      setRetriedGroup(value === RETRY_ALL_FAILED ? RETRY_ALL_FAILED : value);

      try {
        await k8sPatch({
          model: drExecutionModel,
          resource: { metadata: { name: executionName } },
          data: [
            {
              op: 'add',
              path: '/metadata/annotations/soteria.io~1retry-groups',
              value,
            },
          ],
        });
      } catch (e) {
        const message = e instanceof Error ? e.message : String(e);
        setRetryError(message);
        setIsRetrying(false);
      }
    },
    [executionName],
  );

  const retry = useCallback(
    (groupName: string) => patchAnnotation(groupName),
    [patchAnnotation],
  );

  const retryAll = useCallback(
    () => patchAnnotation(RETRY_ALL_FAILED),
    [patchAnnotation],
  );

  useEffect(() => {
    if (!execution) return;

    const rejectedMessage = getRetryRejectedMessage(execution.status?.conditions);

    if (rejectedMessage) {
      errorFromConditionRef.current = true;
      setRetryError(rejectedMessage);
      setIsRetrying(false);
      return;
    }

    if (errorFromConditionRef.current && retryError) {
      errorFromConditionRef.current = false;
      setRetryError(null);
      setRetriedGroup(null);
    }

    if (!isRetrying) return;

    const hasInProgressGroup = execution.status?.waves?.some((w) =>
      w.groups?.some((g) => g.result === 'InProgress'),
    );
    if (hasInProgressGroup) {
      setIsRetrying(false);
    }
  }, [isRetrying, retryError, execution, execution?.status?.conditions, execution?.status?.waves]);

  return { retry, retryAll, isRetrying, retryError, retriedGroup };
}

export { RETRY_GROUPS_ANNOTATION, RETRY_ALL_FAILED, getRetryRejectedMessage };
