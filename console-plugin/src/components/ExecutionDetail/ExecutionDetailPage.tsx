import { useEffect, useRef } from 'react';
import { DocumentTitle } from '@openshift-console/dynamic-plugin-sdk';
import {
  PageSection,
  ProgressStepper,
  Skeleton,
  Alert,
} from '@patternfly/react-core';
import { useParams } from 'react-router-dom';
import DRBreadcrumb from '../shared/DRBreadcrumb';
import { useDRExecution } from '../../hooks/useDRResources';
import ExecutionHeader from './ExecutionHeader';
import WaveProgressStep from './WaveProgressStep';

const ExecutionDetailPage: React.FC = () => {
  const { name } = useParams<{ name: string }>();
  const [execution, execLoaded, execError] = useDRExecution(name);
  const planName = execution?.spec?.planName ?? '';

  const waves = execution?.status?.waves ?? [];
  const completedCount = waves.filter((w) => w.completionTime).length;
  const prevCompletedRef = useRef(completedCount);
  const ariaRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!ariaRef.current) return;
    const prev = prevCompletedRef.current;
    prevCompletedRef.current = completedCount;

    if (prev === completedCount) return;

    const isAllDone = execution?.status?.completionTime;
    if (isAllDone) {
      const resultText = execution?.status?.result ?? 'Unknown';
      ariaRef.current.textContent = `Execution completed. Result: ${resultText}.`;
    } else if (completedCount > prev) {
      const nextWave = completedCount + 1;
      if (nextWave <= waves.length) {
        ariaRef.current.textContent = `Wave ${completedCount} completed. Wave ${nextWave} starting.`;
      } else {
        ariaRef.current.textContent = `Wave ${completedCount} completed.`;
      }
    }
  }, [completedCount, execution?.status?.completionTime, execution?.status?.result, waves.length]);

  if (execError) {
    return (
      <>
        <DocumentTitle>{`DR Execution: ${name}`}</DocumentTitle>
        <PageSection>
          <DRBreadcrumb executionName={name} />
          <Alert variant="danger" isInline title="Failed to load execution">
            {(execError as Error)?.message || String(execError)}
          </Alert>
        </PageSection>
      </>
    );
  }

  if (!execLoaded || !execution) {
    return (
      <>
        <DocumentTitle>{`DR Execution: ${name}`}</DocumentTitle>
        <PageSection>
          <DRBreadcrumb executionName={name} />
          <Skeleton screenreaderText="Loading execution details" height="40px" style={{ marginBottom: 'var(--pf-t--global--spacer--md, var(--pf-v5-global--spacer--md))' }} />
          <Skeleton height="20px" width="60%" style={{ marginBottom: 'var(--pf-t--global--spacer--sm, var(--pf-v5-global--spacer--sm))' }} />
          <Skeleton height="200px" />
        </PageSection>
      </>
    );
  }

  return (
    <>
      <DocumentTitle>{`DR Execution: ${execution.metadata?.name}`}</DocumentTitle>
      <PageSection>
        <DRBreadcrumb planName={planName} executionName={execution.metadata?.name} />
        <ExecutionHeader execution={execution} />
        <ProgressStepper isVertical aria-label="Execution wave progress">
          {waves.map((wave, idx) => (
            <WaveProgressStep key={wave.waveIndex ?? idx} wave={wave} index={idx} />
          ))}
        </ProgressStepper>
        <div
          ref={ariaRef}
          aria-live="polite"
          role="status"
          style={{
            position: 'absolute',
            width: '1px',
            height: '1px',
            overflow: 'hidden',
            clip: 'rect(0, 0, 0, 0)',
            whiteSpace: 'nowrap',
          }}
        />
      </PageSection>
    </>
  );
};

export default ExecutionDetailPage;
