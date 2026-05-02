import { K8sResourceCommon } from '@openshift-console/dynamic-plugin-sdk';

// DRPlan phase values — 8-phase symmetric lifecycle.
// Rest states are persisted on DRPlan.status.phase; transition states are
// derived at runtime via getEffectivePhase().
export const Phase = {
  SteadyState: 'SteadyState',
  FailingOver: 'FailingOver',
  FailedOver: 'FailedOver',
  Reprotecting: 'Reprotecting',
  DRedSteadyState: 'DRedSteadyState',
  FailingBack: 'FailingBack',
  FailedBack: 'FailedBack',
  ReprotectingBack: 'ReprotectingBack',
} as const;

export type DRPhase = (typeof Phase)[keyof typeof Phase];

export const ExecutionMode = {
  PlannedMigration: 'planned_migration',
  Disaster: 'disaster',
  Reprotect: 'reprotect',
} as const;

export type DRExecutionMode = (typeof ExecutionMode)[keyof typeof ExecutionMode];

export const ExecutionResult = {
  Succeeded: 'Succeeded',
  PartiallySucceeded: 'PartiallySucceeded',
  Failed: 'Failed',
} as const;

export type DRExecutionResult = (typeof ExecutionResult)[keyof typeof ExecutionResult];

export const DRGroupResultValue = {
  Pending: 'Pending',
  InProgress: 'InProgress',
  Completed: 'Completed',
  Failed: 'Failed',
  WaitingForVMReady: 'WaitingForVMReady',
} as const;

export type DRGroupResult = (typeof DRGroupResultValue)[keyof typeof DRGroupResultValue];

export const VolumeGroupHealthStatus = {
  Healthy: 'Healthy',
  Degraded: 'Degraded',
  Syncing: 'Syncing',
  NotReplicating: 'NotReplicating',
  Error: 'Error',
  Unknown: 'Unknown',
} as const;

export type VGHealthStatus = (typeof VolumeGroupHealthStatus)[keyof typeof VolumeGroupHealthStatus];

// --- Condition (matches metav1.Condition) ---

export interface Condition {
  type: string;
  status: 'True' | 'False' | 'Unknown';
  lastTransitionTime?: string;
  reason?: string;
  message?: string;
  observedGeneration?: number;
}

// --- DRPlan ---

export interface DRPlan extends K8sResourceCommon {
  spec: DRPlanSpec;
  status?: DRPlanStatus;
}

export interface DRPlanSpec {
  maxConcurrentFailovers: number;
  primarySite: string;
  secondarySite: string;
  vmReadyTimeout?: string;
}

export interface SiteDiscovery {
  vms?: DiscoveredVM[];
  discoveredVMCount?: number;
  lastDiscoveryTime?: string;
}

export interface DRPlanStatus {
  phase?: string;
  activeExecution?: string;
  activeExecutionMode?: DRExecutionMode;
  activeSite?: string;
  conditions?: Condition[];
  observedGeneration?: number;
  waves?: WaveInfo[];
  discoveredVMCount?: number;
  preflight?: PreflightReport;
  replicationHealth?: VolumeGroupHealth[];
  primarySiteDiscovery?: SiteDiscovery;
  secondarySiteDiscovery?: SiteDiscovery;
}

export interface WaveInfo {
  waveKey: string;
  vms: DiscoveredVM[];
  groups?: VolumeGroupInfo[];
}

export interface DiscoveredVM {
  name: string;
  namespace: string;
}

export interface VolumeGroupInfo {
  name: string;
  namespace: string;
  consistencyLevel: 'namespace' | 'vm';
  vmNames: string[];
}

export interface VolumeGroupHealth {
  name: string;
  namespace: string;
  health: VGHealthStatus;
  lastSyncTime?: string;
  lastChecked: string;
  message?: string;
}

export interface PreflightReport {
  primarySite?: string;
  secondarySite?: string;
  activeSite?: string;
  activeExecution?: string;
  waves?: PreflightWave[];
  totalVMs: number;
  warnings?: string[];
  generatedAt?: string;
  sitesInSync?: boolean;
  siteDiscoveryDelta?: string;
}

export interface PreflightWave {
  waveKey: string;
  vmCount: number;
  vms?: PreflightVM[];
  chunks?: PreflightChunk[];
}

export interface PreflightVM {
  name: string;
  namespace: string;
  storageBackend: string;
  consistencyLevel: string;
  volumeGroupName: string;
}

export interface PreflightChunk {
  name: string;
  vmCount: number;
  vmNames?: string[];
  volumeGroups?: string[];
}

// --- DRExecution ---

export interface DRExecution extends K8sResourceCommon {
  spec: DRExecutionSpec;
  status?: DRExecutionStatus;
}

export interface DRExecutionSpec {
  planName: string;
  mode: DRExecutionMode;
}

export interface DRExecutionStatus {
  result?: DRExecutionResult;
  waves?: WaveStatus[];
  startTime?: string;
  completionTime?: string;
  conditions?: Condition[];
}

export interface WaveStatus {
  waveIndex: number;
  groups?: DRGroupExecutionStatus[];
  startTime?: string;
  completionTime?: string;
  vmReadyStartTime?: string;
}

export interface DRGroupExecutionStatus {
  name: string;
  result?: DRGroupResult;
  vmNames?: string[];
  error?: string;
  steps?: StepStatus[];
  retryCount?: number;
  startTime?: string;
  completionTime?: string;
}

export interface StepStatus {
  name: string;
  status?: string;
  message?: string;
  timestamp?: string;
}

// --- DRGroupStatus ---

export interface DRGroupStatus extends K8sResourceCommon {
  spec: DRGroupStatusSpec;
  status?: DRGroupStatusState;
}

export interface DRGroupStatusSpec {
  executionName: string;
  waveIndex: number;
  groupName: string;
  vmNames?: string[];
}

export interface DRGroupStatusState {
  phase?: DRGroupResult;
  conditions?: Condition[];
  steps?: StepStatus[];
  lastTransitionTime?: string;
}
