import { useEffect, useState } from 'react';
import { Alert } from '@patternfly/react-core';
import { Link } from 'react-router';
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
  const currentWave = Math.min(completedWaves + 1, totalWaves);
  const waveProgress = totalWaves > 0
    ? `Wave ${currentWave} of ${totalWaves}`
    : 'Starting...';

  let estimatedRemaining = 'calculating...';
  if (totalWaves > 0 && completedWaves > 0 && startTime) {
    const elapsedMs = Date.now() - new Date(startTime).getTime();
    const avgPerWave = elapsedMs / completedWaves;
    const remainingMs = (totalWaves - completedWaves) * avgPerWave;
    estimatedRemaining = `~${formatElapsed(remainingMs)}`;
  }

  return (
    <div style={{ marginBottom: 'var(--pf-v5-global--spacer--md)' }}>
      <Alert
        variant="info"
        isInline
        title={`${transition.action} in progress`}
        actionLinks={
          <Link to={`/disaster-recovery/executions/${plan.status?.activeExecution}`}>
            View execution details
          </Link>
        }
      >
        <span style={{ fontSize: 'var(--pf-v5-global--FontSize--lg)' }}>{waveProgress}</span>
        {' — Elapsed: '}
        <span style={{ fontSize: 'var(--pf-v5-global--FontSize--lg)' }}>{elapsed || '0s'}</span>
        {' · Est. remaining: '}
        <span style={{ fontSize: 'var(--pf-v5-global--FontSize--lg)' }}>{estimatedRemaining}</span>
      </Alert>
    </div>
  );
};

export default TransitionProgressBanner;
