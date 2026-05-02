import { CSSProperties, useMemo } from 'react';
import { Button, Tooltip } from '@patternfly/react-core';
import { DRPlan } from '../../models/types';
import { getEffectivePhase, RestPhase, TransientPhase } from '../../utils/drPlanUtils';
import { isTransientPhase, getValidActions, DRAction } from '../../utils/drPlanActions';

import steadyStateImg from '../../assets/state-steady-state.png';
import failedOverImg from '../../assets/state-failed-over.png';
import dredSteadyStateImg from '../../assets/state-dred-steady-state.png';
import failedBackImg from '../../assets/state-failed-back.png';

const PHASE_IMAGES: Record<string, string> = {
  SteadyState: steadyStateImg,
  FailedOver: failedOverImg,
  DRedSteadyState: dredSteadyStateImg,
  FailedBack: failedBackImg,
};

interface PhaseInfo {
  id: RestPhase;
  label: string;
  activeLabel: (primary: string, secondary: string) => string;
  passiveLabel: (primary: string, secondary: string) => string;
  replication: string;
}

const REST_PHASES: PhaseInfo[] = [
  {
    id: 'SteadyState',
    label: 'Steady State',
    activeLabel: (p) => `VMs running in ${p}`,
    passiveLabel: (_p, s) => `VMs stopped in ${s}`,
    replication: 'on',
  },
  {
    id: 'FailedOver',
    label: 'Failed Over',
    activeLabel: (_p, s) => `VMs running in ${s}`,
    passiveLabel: (p) => `VMs stopped in ${p}`,
    replication: 'off',
  },
  {
    id: 'DRedSteadyState',
    label: 'DR-ed Steady State',
    activeLabel: (_p, s) => `VMs running in ${s}`,
    passiveLabel: (p) => `VMs stopped in ${p}`,
    replication: 'on',
  },
  {
    id: 'FailedBack',
    label: 'Failed Back',
    activeLabel: (p) => `VMs running in ${p}`,
    passiveLabel: (_p, s) => `VMs stopped in ${s}`,
    replication: 'off',
  },
];

export const TRANSITIONS = [
  { from: 'SteadyState' as RestPhase, to: 'FailedOver' as RestPhase, transient: 'FailingOver' as TransientPhase },
  { from: 'FailedOver' as RestPhase, to: 'DRedSteadyState' as RestPhase, transient: 'Reprotecting' as TransientPhase },
  { from: 'DRedSteadyState' as RestPhase, to: 'FailedBack' as RestPhase, transient: 'FailingBack' as TransientPhase },
  { from: 'FailedBack' as RestPhase, to: 'SteadyState' as RestPhase, transient: 'Restoring' as TransientPhase },
] as const;

interface PhaseNodeProps {
  phase: PhaseInfo;
  primarySite: string;
  secondarySite: string;
  isActive: boolean;
  isTransitioning: boolean;
}

function PhaseNode({ phase, primarySite, secondarySite, isActive, isTransitioning }: PhaseNodeProps) {
  const borderColor = isActive || isTransitioning
    ? 'var(--pf-t--global--color--brand--default, var(--pf-v5-global--active-color--100))'
    : 'var(--pf-t--global--border--color--default, var(--pf-v5-global--BorderColor--100))';

  const nodeStyle: CSSProperties = {
    borderWidth: '2px',
    borderStyle: isTransitioning ? 'dashed' : 'solid',
    borderColor,
    borderRadius: 'var(--pf-t--global--border--radius--small, var(--pf-v5-global--BorderRadius--sm))',
    padding: 'var(--pf-t--global--spacer--xs, var(--pf-v5-global--spacer--xs)) var(--pf-t--global--spacer--sm, var(--pf-v5-global--spacer--sm))',
    background: isActive ? 'var(--pf-t--global--color--brand--default, var(--pf-v5-global--active-color--100))' : 'transparent',
    opacity: (isActive || isTransitioning) ? 1 : 0.35,
    color: isActive ? 'var(--pf-t--global--text--color--inverse, var(--pf-v5-global--Color--light-100))' : 'var(--pf-t--global--text--color--regular, var(--pf-v5-global--Color--100))',
    textAlign: 'center' as const,
  };

  const activeText = phase.activeLabel(primarySite, secondarySite);
  const passiveText = phase.passiveLabel(primarySite, secondarySite);
  const replicationText = `Volume Replication: ${phase.replication}`;
  const imgSrc = PHASE_IMAGES[phase.id];

  return (
    <div
      role="group"
      aria-label={`${phase.label}, ${isActive ? 'current phase, ' : ''}${isTransitioning ? 'transition destination, ' : ''}${activeText}, ${replicationText}`}
      style={nodeStyle}
      data-testid={`phase-node-${phase.id}`}
    >
      <div style={{ fontWeight: 'var(--pf-t--global--font--weight--body--bold, var(--pf-v5-global--FontWeight--bold))' as unknown as number, fontSize: 'var(--pf-t--global--font--size--body--default, var(--pf-v5-global--FontSize--md))', marginBottom: 'var(--pf-t--global--spacer--xs, var(--pf-v5-global--spacer--xs))' }}>
        {phase.label}
      </div>
      <div style={{ fontSize: 'var(--pf-t--global--font--size--body--sm, var(--pf-v5-global--FontSize--sm))' }}>
        {activeText}
      </div>
      <div style={{ fontSize: 'var(--pf-t--global--font--size--body--sm, var(--pf-v5-global--FontSize--sm))' }}>
        {passiveText}
      </div>
      <div style={{ fontSize: 'var(--pf-t--global--font--size--body--sm, var(--pf-v5-global--FontSize--sm))' }}>
        {replicationText}
      </div>
      {imgSrc && (
        <img
          src={imgSrc}
          alt={`${phase.label} topology: ${activeText}, ${replicationText}`}
          style={{
            marginTop: 'var(--pf-t--global--spacer--sm, var(--pf-v5-global--spacer--sm))',
            maxWidth: '37.5%',
            height: 'auto',
            borderRadius: 'var(--pf-t--global--border--radius--small, var(--pf-v5-global--BorderRadius--sm))',
          }}
        />
      )}
    </div>
  );
}

interface TransitionEdgeProps {
  transition: typeof TRANSITIONS[number];
  state: 'idle' | 'available' | 'in-progress';
  actions: DRAction[];
  plan: DRPlan;
  onAction: (action: string, plan: DRPlan) => void;
  direction: 'horizontal' | 'vertical';
  isBlocked?: boolean;
  blockedTooltip?: string;
}

function TransitionEdge({ transition, state, actions, plan, onAction, direction, isBlocked, blockedTooltip }: TransitionEdgeProps) {
  const isHorizontal = direction === 'horizontal';

  const containerStyle: CSSProperties = {
    display: 'flex',
    flexDirection: isHorizontal ? 'column' : 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 'var(--pf-t--global--spacer--xs, var(--pf-v5-global--spacer--xs))',
    padding: 'var(--pf-t--global--spacer--sm, var(--pf-v5-global--spacer--sm))',
    minWidth: isHorizontal ? '160px' : undefined,
    minHeight: !isHorizontal ? '60px' : undefined,
  };

  const arrowChar = isHorizontal ? '\u2192' : '\u2193';
  const reverseArrowChar = isHorizontal ? '\u2190' : '\u2191';

  const isForward = transition.from === 'SteadyState' || transition.from === 'FailedOver';
  const arrow = isForward ? arrowChar : reverseArrowChar;

  const transitionLabel = transition.transient.replace(/([a-z])([A-Z])/g, '$1 $2');

  return (
    <div style={containerStyle} data-testid={`transition-${transition.from}-${transition.to}`}>
      {state === 'available' && (
        <>
          {actions.map((a) => {
            const btn = (
              <Button
                key={a.key}
                variant={a.isDanger ? 'danger' : 'secondary'}
                onClick={() => onAction(a.key, plan)}
                size="sm"
                isDisabled={isBlocked}
              >
                {a.label}
              </Button>
            );
            return isBlocked && blockedTooltip ? (
              <Tooltip key={a.key} content={blockedTooltip}>
                <span>{btn}</span>
              </Tooltip>
            ) : (
              btn
            );
          })}
          <span style={{ fontSize: 'var(--pf-t--global--font--size--body--lg, var(--pf-v5-global--FontSize--lg))', margin: '0 var(--pf-t--global--spacer--xs, var(--pf-v5-global--spacer--xs))' }}>{arrow}</span>
        </>
      )}
      {state === 'in-progress' && (
        <span
          style={{
            background: 'var(--pf-t--global--color--status--info--default, var(--pf-v5-global--info-color--100))',
            color: 'var(--pf-t--global--text--color--inverse, var(--pf-v5-global--Color--light-100))',
            padding: 'var(--pf-t--global--spacer--xs, var(--pf-v5-global--spacer--xs)) var(--pf-t--global--spacer--sm, var(--pf-v5-global--spacer--sm))',
            borderRadius: 'var(--pf-t--global--border--radius--large, var(--pf-v5-global--BorderRadius--lg))',
            fontSize: 'var(--pf-t--global--font--size--body--default, var(--pf-v5-global--FontSize--md))',
          }}
        >
          In progress...
        </span>
      )}
      {state === 'idle' && (
        <span style={{ opacity: 0.35, fontSize: 'var(--pf-t--global--font--size--body--default, var(--pf-v5-global--FontSize--md))' }}>
          {transitionLabel} {arrow}
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
  isBlocked?: boolean;
  blockedTooltip?: string;
}

const DRLifecycleDiagram: React.FC<DRLifecycleDiagramProps> = ({ plan, onAction, waveProgress, isBlocked, blockedTooltip }) => {
  const restPhase = (plan.status?.phase ?? 'SteadyState') as RestPhase;
  const effectivePhase = getEffectivePhase(plan);
  const inTransition = isTransientPhase(effectivePhase);
  const primarySite = plan.spec?.primarySite ?? 'Primary';
  const secondarySite = plan.spec?.secondarySite ?? 'Secondary';

  const validActions = useMemo(() => getValidActions(plan), [plan]);

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

  function getEdgeActions(t: typeof TRANSITIONS[number]): DRAction[] {
    if (t.from !== restPhase || inTransition) return [];
    return validActions;
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
    gap: 'var(--pf-t--global--spacer--lg, var(--pf-v5-global--spacer--lg))',
    alignItems: 'center',
    justifyItems: 'center',
    padding: 'var(--pf-t--global--spacer--lg, var(--pf-v5-global--spacer--lg))',
  };

  const activeTransitionAction = activeTransition
    ? activeTransition.transient.replace(/([a-z])([A-Z])/g, '$1 $2')
    : '';

  return (
    <div
      role="figure"
      aria-label="DR lifecycle state machine diagram"
      style={gridStyle}
      data-testid="dr-lifecycle-diagram"
    >
      {/* Row 1: SteadyState -> Failover -> FailedOver */}
      <PhaseNode phase={steadyState} primarySite={primarySite} secondarySite={secondarySite} isActive={isActivePhase('SteadyState')} isTransitioning={isDestination('SteadyState')} />
      <TransitionEdge transition={failoverT} state={getEdgeState(failoverT)} actions={getEdgeActions(failoverT)} plan={plan} onAction={onAction} direction="horizontal" isBlocked={isBlocked} blockedTooltip={blockedTooltip} />
      <PhaseNode phase={failedOver} primarySite={primarySite} secondarySite={secondarySite} isActive={isActivePhase('FailedOver')} isTransitioning={isDestination('FailedOver')} />

      {/* Row 2: Restore (vertical up) | empty | Reprotect (vertical down) */}
      <TransitionEdge transition={restoreT} state={getEdgeState(restoreT)} actions={getEdgeActions(restoreT)} plan={plan} onAction={onAction} direction="vertical" isBlocked={isBlocked} blockedTooltip={blockedTooltip} />
      <div />
      <TransitionEdge transition={reprotectT} state={getEdgeState(reprotectT)} actions={getEdgeActions(reprotectT)} plan={plan} onAction={onAction} direction="vertical" isBlocked={isBlocked} blockedTooltip={blockedTooltip} />

      {/* Row 3: FailedBack <- Failback <- DRedSteadyState */}
      <PhaseNode phase={failedBack} primarySite={primarySite} secondarySite={secondarySite} isActive={isActivePhase('FailedBack')} isTransitioning={isDestination('FailedBack')} />
      <TransitionEdge transition={failbackT} state={getEdgeState(failbackT)} actions={getEdgeActions(failbackT)} plan={plan} onAction={onAction} direction="horizontal" isBlocked={isBlocked} blockedTooltip={blockedTooltip} />
      <PhaseNode phase={dRedSteadyState} primarySite={primarySite} secondarySite={secondarySite} isActive={isActivePhase('DRedSteadyState')} isTransitioning={isDestination('DRedSteadyState')} />

      {/* ARIA live region for transition progress */}
      <div
        aria-live="polite"
        role="status"
        style={{ position: 'absolute', left: '-9999px', width: '1px', height: '1px', overflow: 'hidden' }}
      >
        {inTransition && activeTransition
          ? waveProgress
            ? `${activeTransitionAction} in progress, wave ${waveProgress.current} of ${waveProgress.total}`
            : `${activeTransitionAction} in progress`
          : ''}
      </div>
    </div>
  );
};

export default DRLifecycleDiagram;
