import { CSSProperties, useMemo } from 'react';
import { Button } from '@patternfly/react-core';
import { DRPlan } from '../../models/types';
import { getEffectivePhase, RestPhase, TransientPhase } from '../../utils/drPlanUtils';
import { isTransientPhase } from '../../utils/drPlanActions';

const REST_PHASES = [
  { id: 'SteadyState' as const, label: 'Steady State', description: 'Normal operations',
    vm: 'VMs on DC1', dc1: 'Active (source)', dc2: 'Passive (target)', replication: 'DC1 → DC2' },
  { id: 'FailedOver' as const, label: 'Failed Over', description: 'Running on DR site',
    vm: 'VMs on DC2', dc1: 'Passive / down', dc2: 'Active (promoted)', replication: 'None' },
  { id: 'DRedSteadyState' as const, label: 'DR-ed Steady State', description: 'Protected on DR site',
    vm: 'VMs on DC2', dc1: 'Passive (target)', dc2: 'Active (source)', replication: 'DC2 → DC1' },
  { id: 'FailedBack' as const, label: 'Failed Back', description: 'Returned to origin',
    vm: 'VMs on DC1', dc1: 'Active (promoted)', dc2: 'Passive / down', replication: 'None' },
] as const;

export const TRANSITIONS = [
  { from: 'SteadyState' as RestPhase, to: 'FailedOver' as RestPhase, action: 'Failover', transient: 'FailingOver' as TransientPhase, isDanger: true },
  { from: 'FailedOver' as RestPhase, to: 'DRedSteadyState' as RestPhase, action: 'Reprotect', transient: 'Reprotecting' as TransientPhase, isDanger: false },
  { from: 'DRedSteadyState' as RestPhase, to: 'FailedBack' as RestPhase, action: 'Failback', transient: 'FailingBack' as TransientPhase, isDanger: false },
  { from: 'FailedBack' as RestPhase, to: 'SteadyState' as RestPhase, action: 'Restore', transient: 'Restoring' as TransientPhase, isDanger: false },
] as const;

interface PhaseNodeProps {
  phase: typeof REST_PHASES[number];
  isActive: boolean;
  isTransitioning: boolean;
}

function PhaseNode({ phase, isActive, isTransitioning }: PhaseNodeProps) {
  const borderColor = isActive || isTransitioning
    ? 'var(--pf-v5-global--active-color--100)'
    : 'var(--pf-v5-global--BorderColor--100)';

  const nodeStyle: CSSProperties = {
    borderWidth: '2px',
    borderStyle: isTransitioning ? 'dashed' : 'solid',
    borderColor,
    borderRadius: 'var(--pf-v5-global--BorderRadius--sm)',
    padding: 'var(--pf-v5-global--spacer--md)',
    background: isActive ? 'var(--pf-v5-global--active-color--100)' : 'transparent',
    opacity: (isActive || isTransitioning) ? 1 : 0.35,
    color: isActive ? 'var(--pf-v5-global--Color--light-100)' : 'var(--pf-v5-global--Color--100)',
    minWidth: '220px',
    textAlign: 'center' as const,
  };

  return (
    <div
      role="group"
      aria-label={`${phase.label}, ${isActive ? 'current phase, ' : ''}${isTransitioning ? 'transition destination, ' : ''}VMs on ${phase.vm}, replication: ${phase.replication}`}
      style={nodeStyle}
      data-testid={`phase-node-${phase.id}`}
    >
      <div style={{ fontWeight: 'var(--pf-v5-global--FontWeight--bold)' as unknown as number, marginBottom: 'var(--pf-v5-global--spacer--xs)' }}>
        {phase.label}
      </div>
      <div style={{ fontSize: 'var(--pf-v5-global--FontSize--sm)', opacity: 0.85 }}>
        {phase.description}
      </div>
      <div style={{ fontSize: 'var(--pf-v5-global--FontSize--sm)', marginTop: 'var(--pf-v5-global--spacer--xs)' }}>
        {phase.vm}
      </div>
      <div style={{ fontSize: 'var(--pf-v5-global--FontSize--xs)' }}>
        DC1: {phase.dc1}
      </div>
      <div style={{ fontSize: 'var(--pf-v5-global--FontSize--xs)' }}>
        DC2: {phase.dc2}
      </div>
      <div style={{ fontSize: 'var(--pf-v5-global--FontSize--xs)' }}>
        Replication: {phase.replication}
      </div>
    </div>
  );
}

interface TransitionEdgeProps {
  transition: typeof TRANSITIONS[number];
  state: 'idle' | 'available' | 'in-progress';
  plan: DRPlan;
  onAction: (action: string, plan: DRPlan) => void;
  direction: 'horizontal' | 'vertical';
}

function TransitionEdge({ transition, state, plan, onAction, direction }: TransitionEdgeProps) {
  const isHorizontal = direction === 'horizontal';

  const containerStyle: CSSProperties = {
    display: 'flex',
    flexDirection: isHorizontal ? 'column' : 'row',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 'var(--pf-v5-global--spacer--sm)',
    minWidth: isHorizontal ? '120px' : undefined,
    minHeight: !isHorizontal ? '60px' : undefined,
  };

  const arrowChar = isHorizontal ? '→' : '↓';
  const reverseArrowChar = isHorizontal ? '←' : '↑';

  const isForward = transition.from === 'SteadyState' || transition.from === 'FailedOver';
  const arrow = isForward ? arrowChar : reverseArrowChar;

  return (
    <div style={containerStyle} data-testid={`transition-${transition.action.toLowerCase()}`}>
      {state === 'available' && (
        <>
          <Button
            variant={transition.isDanger ? 'danger' : 'secondary'}
            onClick={() => onAction(transition.action, plan)}
            size="sm"
          >
            {transition.action}
          </Button>
          <span style={{ fontSize: 'var(--pf-v5-global--FontSize--lg)', margin: '0 var(--pf-v5-global--spacer--xs)' }}>{arrow}</span>
        </>
      )}
      {state === 'in-progress' && (
        <span
          style={{
            background: 'var(--pf-v5-global--info-color--100)',
            color: 'var(--pf-v5-global--Color--light-100)',
            padding: 'var(--pf-v5-global--spacer--xs) var(--pf-v5-global--spacer--sm)',
            borderRadius: 'var(--pf-v5-global--BorderRadius--lg)',
            fontSize: 'var(--pf-v5-global--FontSize--sm)',
          }}
        >
          In progress...
        </span>
      )}
      {state === 'idle' && (
        <span style={{ opacity: 0.35, fontSize: 'var(--pf-v5-global--FontSize--sm)' }}>
          {transition.action} {arrow}
        </span>
      )}
    </div>
  );
}

export interface WaveProgress {
  current: number;
  total: number;
}

interface DRLifecycleDiagramProps {
  plan: DRPlan;
  onAction: (action: string, plan: DRPlan) => void;
  waveProgress?: WaveProgress | null;
}

const DRLifecycleDiagram: React.FC<DRLifecycleDiagramProps> = ({ plan, onAction, waveProgress }) => {
  const restPhase = (plan.status?.phase ?? 'SteadyState') as RestPhase;
  const effectivePhase = getEffectivePhase(plan);
  const inTransition = isTransientPhase(effectivePhase);

  const activeTransition = useMemo(
    () => inTransition ? TRANSITIONS.find(t => t.transient === effectivePhase) : null,
    [effectivePhase, inTransition],
  );

  function getEdgeState(t: typeof TRANSITIONS[number]): 'idle' | 'available' | 'in-progress' {
    if (inTransition) {
      return t === activeTransition ? 'in-progress' : 'idle';
    }
    return t.from === restPhase ? 'available' : 'idle';
  }

  const steadyState = REST_PHASES[0];
  const failedOver = REST_PHASES[1];
  const dRedSteadyState = REST_PHASES[2];
  const failedBack = REST_PHASES[3];

  const failoverT = TRANSITIONS[0];
  const reprotectT = TRANSITIONS[1];
  const failbackT = TRANSITIONS[2];
  const restoreT = TRANSITIONS[3];

  function isActivePhase(phaseId: string): boolean {
    return phaseId === restPhase;
  }

  function isDestination(phaseId: string): boolean {
    return inTransition && activeTransition?.to === phaseId;
  }

  const gridStyle: CSSProperties = {
    display: 'grid',
    gridTemplateColumns: '1fr auto 1fr',
    gridTemplateRows: 'auto auto auto',
    gap: 'var(--pf-v5-global--spacer--lg)',
    alignItems: 'center',
    justifyItems: 'center',
    padding: 'var(--pf-v5-global--spacer--lg)',
  };

  return (
    <div
      role="figure"
      aria-label="DR lifecycle state machine diagram"
      style={gridStyle}
      data-testid="dr-lifecycle-diagram"
    >
      {/* Row 1: SteadyState -> Failover -> FailedOver */}
      <PhaseNode phase={steadyState} isActive={isActivePhase('SteadyState')} isTransitioning={isDestination('SteadyState')} />
      <TransitionEdge transition={failoverT} state={getEdgeState(failoverT)} plan={plan} onAction={onAction} direction="horizontal" />
      <PhaseNode phase={failedOver} isActive={isActivePhase('FailedOver')} isTransitioning={isDestination('FailedOver')} />

      {/* Row 2: Restore (vertical up) | empty | Reprotect (vertical down) */}
      <TransitionEdge transition={restoreT} state={getEdgeState(restoreT)} plan={plan} onAction={onAction} direction="vertical" />
      <div />
      <TransitionEdge transition={reprotectT} state={getEdgeState(reprotectT)} plan={plan} onAction={onAction} direction="vertical" />

      {/* Row 3: FailedBack <- Failback <- DRedSteadyState */}
      <PhaseNode phase={failedBack} isActive={isActivePhase('FailedBack')} isTransitioning={isDestination('FailedBack')} />
      <TransitionEdge transition={failbackT} state={getEdgeState(failbackT)} plan={plan} onAction={onAction} direction="horizontal" />
      <PhaseNode phase={dRedSteadyState} isActive={isActivePhase('DRedSteadyState')} isTransitioning={isDestination('DRedSteadyState')} />

      {/* ARIA live region for transition progress */}
      <div
        aria-live="polite"
        role="status"
        style={{ position: 'absolute', left: '-9999px', width: '1px', height: '1px', overflow: 'hidden' }}
      >
        {inTransition && activeTransition
          ? waveProgress
            ? `${activeTransition.action} in progress, wave ${waveProgress.current} of ${waveProgress.total}`
            : `${activeTransition.action} in progress`
          : ''}
      </div>
    </div>
  );
};

export default DRLifecycleDiagram;
