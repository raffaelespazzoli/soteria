import { DRExecutionMode, DRPlan } from '../models/types';
import { EffectivePhase, getEffectivePhase } from './drPlanUtils';

export interface DRAction {
  key: string;
  label: string;
  isDanger?: boolean;
}

export interface ActionConfig {
  label: string;
  keyword: string;
  mode: DRExecutionMode;
  confirmVariant: 'danger' | 'primary';
}

export const ACTION_CONFIG: Record<string, ActionConfig> = {
  failover: { label: 'Failover', keyword: 'FAILOVER', mode: 'disaster', confirmVariant: 'danger' },
  planned_migration: { label: 'Planned Migration', keyword: 'MIGRATE', mode: 'planned_migration', confirmVariant: 'primary' },
  reprotect: { label: 'Reprotect', keyword: 'REPROTECT', mode: 'reprotect', confirmVariant: 'primary' },
  failback: { label: 'Failback', keyword: 'FAILBACK', mode: 'planned_migration', confirmVariant: 'primary' },
  planned_failback: { label: 'Planned Migration', keyword: 'MIGRATE', mode: 'planned_migration', confirmVariant: 'primary' },
  restore: { label: 'Restore', keyword: 'RESTORE', mode: 'reprotect', confirmVariant: 'primary' },
};

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

const LABEL_TO_KEY: Record<string, string> = Object.fromEntries(
  Object.entries(ACTION_CONFIG).map(([key, cfg]) => [cfg.label, key]),
);

export function resolveActionKey(action: string): string {
  if (ACTION_CONFIG[action]) return action;
  return LABEL_TO_KEY[action] ?? action;
}

export function getValidActions(plan: DRPlan): DRAction[] {
  const phase = getEffectivePhase(plan);
  return ACTIONS_BY_PHASE[phase] ?? [];
}

export function isTransientPhase(phase: EffectivePhase): boolean {
  return ['FailingOver', 'Reprotecting', 'FailingBack', 'Restoring'].includes(phase);
}
