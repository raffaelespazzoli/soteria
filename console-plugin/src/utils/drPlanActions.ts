import { DRPlan } from '../models/types';
import { EffectivePhase, getEffectivePhase } from './drPlanUtils';

export interface DRAction {
  key: string;
  label: string;
  isDanger?: boolean;
}

const ACTIONS_BY_PHASE: Record<string, DRAction[]> = {
  SteadyState: [
    { key: 'failover', label: 'Failover', isDanger: true },
    { key: 'planned_migration', label: 'Planned Migration' },
  ],
  FailedOver: [{ key: 'reprotect', label: 'Reprotect' }],
  DRedSteadyState: [
    { key: 'failback', label: 'Failback', isDanger: true },
    { key: 'planned_failback', label: 'Planned Migration' },
  ],
  FailedBack: [{ key: 'restore', label: 'Restore' }],
};

export function getValidActions(plan: DRPlan): DRAction[] {
  const phase = getEffectivePhase(plan);
  return ACTIONS_BY_PHASE[phase] ?? [];
}

export function isTransientPhase(phase: EffectivePhase): boolean {
  return ['FailingOver', 'Reprotecting', 'FailingBack', 'Restoring'].includes(phase);
}
