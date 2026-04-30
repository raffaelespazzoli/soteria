import { useEffect, useState } from 'react';
import { Button, Progress, ProgressMeasureLocation } from '@patternfly/react-core';
import { useHistory } from 'react-router-dom';
import { DRExecution, DRPlan } from '../../models/types';
import { getEffectivePhase } from '../../utils/drPlanUtils';
import { TRANSITIONS } from './DRLifecycleDiagram';

interface TransitionProgressBannerProps {
  plan: DRPlan;
  execution: DRExecution | null;
}

function formatElapsed(ms: number): string {
  if (ms < 0) return '0s';
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  if (minutes < 60) return `${minutes}m ${remainingSeconds}s`;
  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  return `${hours}h ${remainingMinutes}m`;
}

const TransitionProgressBanner: React.FC<TransitionProgressBannerProps> = ({ plan, execution }) => {
  const history = useHistory();
  const effectivePhase = getEffectivePhase(plan);
  const restPhase = plan.status?.phase;
  const [elapsed, setElapsed] = useState('');
  const startTime = execution?.status?.startTime;

  useEffect(() => {
    if (!startTime) return;

    function update() {
      const ms = Date.now() - new Date(startTime!).getTime();
      setElapsed(formatElapsed(ms));
    }
    update();
    const timer = setInterval(update, 1000);
    return () => clearInterval(timer);
  }, [startTime]);

  if (effectivePhase === restPhase) return null;

  const transition = TRANSITIONS.find(t => t.transient === effectivePhase);
  if (!transition) return null;

  const waves = execution?.status?.waves;
  const completedWaves = waves ? waves.filter(w => w.completionTime).length : 0;
  const totalWaves = waves?.length ?? 0;
  const pctComplete = totalWaves > 0 ? Math.round((completedWaves / totalWaves) * 100) : 0;

  const transitionLabel = transition.transient.replace(/([a-z])([A-Z])/g, '$1 $2');

  let waveLabel: string;
  if (totalWaves === 0) {
    waveLabel = 'Starting...';
  } else if (completedWaves >= totalWaves) {
    waveLabel = `${totalWaves} of ${totalWaves} waves complete — finalizing`;
  } else {
    waveLabel = `Wave ${completedWaves + 1} of ${totalWaves}`;
  }

  let estimatedRemaining = 'calculating...';
  if (totalWaves > 0 && completedWaves > 0 && startTime) {
    const elapsedMs = Date.now() - new Date(startTime).getTime();
    const avgPerWave = elapsedMs / completedWaves;
    const remainingMs = (totalWaves - completedWaves) * avgPerWave;
    estimatedRemaining = `~${formatElapsed(remainingMs)}`;
  }

  const activeExec = plan.status?.activeExecution;
  // Capture as a string constant at render time so the closure holds
  // the primitive value, immune to object mutation by the SDK.
  const execDetailPath = activeExec ? `/disaster-recovery/executions/${activeExec}` : '';

  return (
    <div style={{ marginBottom: 'var(--pf-v5-global--spacer--md)' }}>
      <Progress
        value={pctComplete}
        title={`${transitionLabel} in progress`}
        measureLocation={ProgressMeasureLocation.top}
        label={`${pctComplete}%`}
        aria-label={`${transitionLabel} progress: ${waveLabel}`}
      />
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          marginTop: 'var(--pf-v5-global--spacer--xs)',
          fontSize: 'var(--pf-v5-global--FontSize--sm)',
          color: 'var(--pf-v5-global--Color--200)',
        }}
      >
        <span>
          {waveLabel}
          {' — Elapsed: '}
          <strong>{elapsed || '0s'}</strong>
          {' · Est. remaining: '}
          <strong>{estimatedRemaining}</strong>
        </span>
        {execDetailPath && (
          <Button
            variant="link"
            isInline
            onClick={() => history.push(execDetailPath)}
          >
            View execution details
          </Button>
        )}
      </div>
    </div>
  );
};

export default TransitionProgressBanner;
