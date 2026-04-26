import { useMemo } from 'react';
import { TreeView, TreeViewDataItem, Label } from '@patternfly/react-core';
import {
  CheckCircleIcon,
  ExclamationTriangleIcon,
  ExclamationCircleIcon,
  QuestionCircleIcon,
  SyncAltIcon,
} from '@patternfly/react-icons';
import ReplicationHealthIndicator from '../shared/ReplicationHealthIndicator';
import { DRPlan, DiscoveredVM, VolumeGroupInfo, VolumeGroupHealth } from '../../models/types';
import { formatRPO } from '../../utils/formatters';
import { ReplicationHealthStatus } from '../../utils/drPlanUtils';

function getVGHealth(
  vgName: string,
  healthData: VolumeGroupHealth[],
): { status: ReplicationHealthStatus; rpoSeconds: number | null } {
  const vg = healthData.find((h) => h.name === vgName);
  if (!vg) return { status: 'Unknown', rpoSeconds: null };
  const rpoMatch = vg.estimatedRPO?.match(/^(\d+)/);
  const parsed = rpoMatch ? parseInt(rpoMatch[1], 10) : NaN;
  const rpoSeconds = isNaN(parsed) ? null : parsed;
  return { status: vg.health as ReplicationHealthStatus, rpoSeconds };
}

function getStorageBackend(
  vmName: string,
  vmNamespace: string,
  plan: DRPlan,
): string {
  const preflightVMs = plan.status?.preflight?.waves?.flatMap(
    (w) => w.vms ?? [],
  );
  const pvm = preflightVMs?.find(
    (v) => v.name === vmName && v.namespace === vmNamespace,
  );
  return pvm?.storageBackend ?? 'unknown';
}

function getAggregateHealth(
  groups: VolumeGroupInfo[],
  healthData: VolumeGroupHealth[],
): ReplicationHealthStatus {
  const statuses = groups.map((g) => getVGHealth(g.name, healthData).status);
  if (statuses.includes('Error')) return 'Error';
  if (statuses.includes('Degraded')) return 'Degraded';
  if (statuses.includes('Syncing')) return 'Syncing';
  if (statuses.includes('Unknown')) return 'Unknown';
  return 'Healthy';
}

const HEALTH_LABEL_COLORS: Record<ReplicationHealthStatus, 'green' | 'yellow' | 'blue' | 'red' | 'grey'> = {
  Healthy: 'green',
  Degraded: 'yellow',
  Syncing: 'blue',
  Error: 'red',
  Unknown: 'grey',
};

const HEALTH_ICONS: Record<ReplicationHealthStatus, React.ReactElement> = {
  Healthy: <CheckCircleIcon />,
  Degraded: <ExclamationTriangleIcon />,
  Syncing: <SyncAltIcon />,
  Error: <ExclamationCircleIcon />,
  Unknown: <QuestionCircleIcon />,
};

function AggregateHealthBadge({ groups, healthData }: { groups: VolumeGroupInfo[]; healthData: VolumeGroupHealth[] }) {
  const statuses = groups.map((g) => getVGHealth(g.name, healthData).status);
  const worst = getAggregateHealth(groups, healthData);

  let label: string;
  if (worst === 'Healthy') {
    label = 'All Healthy';
  } else {
    const counts = statuses.reduce<Record<string, number>>((acc, s) => {
      if (s !== 'Healthy') acc[s] = (acc[s] ?? 0) + 1;
      return acc;
    }, {});
    label = Object.entries(counts)
      .sort(([a], [b]) => (HEALTH_LABEL_COLORS[a as ReplicationHealthStatus] < HEALTH_LABEL_COLORS[b as ReplicationHealthStatus] ? -1 : 1))
      .map(([s, n]) => `${n} ${s}`)
      .join(', ');
  }

  return (
    <Label isCompact color={HEALTH_LABEL_COLORS[worst]} icon={HEALTH_ICONS[worst]}>
      {label}
    </Label>
  );
}

function VMNodeContent({
  vmName,
  namespace,
  consistencyLevel,
  storageBackend,
  health,
}: {
  vmName: string;
  namespace: string;
  consistencyLevel: 'namespace' | 'vm';
  storageBackend: string;
  health: { status: ReplicationHealthStatus; rpoSeconds: number | null };
}) {
  const rpoText = formatRPO(health.rpoSeconds);
  const ariaStr = [
    vmName,
    storageBackend,
    consistencyLevel === 'namespace' ? `namespace ${namespace}` : 'VM-level consistency',
    `replication ${health.status.toLowerCase()}`,
    rpoText || undefined,
  ]
    .filter(Boolean)
    .join(', ');

  return (
    <span
      style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--pf-t--global--spacer--sm)', flexWrap: 'wrap' }}
      aria-label={ariaStr}
    >
      <span style={{ fontWeight: 600 }}>{vmName}</span>
      <Label isCompact>{storageBackend}</Label>
      {consistencyLevel === 'namespace' ? (
        <Label isCompact color="blue">NS: {namespace}</Label>
      ) : (
        <span style={{ fontSize: 'var(--pf-t--global--font--size--body--default)', color: 'var(--pf-t--global--text--color--subtle)' }}>
          VM-level
        </span>
      )}
      <ReplicationHealthIndicator health={health} />
    </span>
  );
}

function buildVMNodes(
  group: VolumeGroupInfo,
  plan: DRPlan,
  healthData: VolumeGroupHealth[],
): TreeViewDataItem[] {
  const vgHealth = getVGHealth(group.name, healthData);
  return (group.vmNames ?? []).map((vmName) => ({
    name: (
      <VMNodeContent
        vmName={vmName}
        namespace={group.namespace}
        consistencyLevel={group.consistencyLevel}
        storageBackend={getStorageBackend(vmName, group.namespace, plan)}
        health={vgHealth}
      />
    ),
    id: `vm-${group.name}-${vmName}`,
  }));
}

function buildDiscoveredVMNodes(
  vms: DiscoveredVM[],
  plan: DRPlan,
  healthData: VolumeGroupHealth[],
): TreeViewDataItem[] {
  return vms.map((vm) => {
    const backend = getStorageBackend(vm.name, vm.namespace, plan);
    const preflightVM = plan.status?.preflight?.waves
      ?.flatMap((w) => w.vms ?? [])
      .find((p) => p.name === vm.name && p.namespace === vm.namespace);
    const vgHealth = preflightVM?.volumeGroupName
      ? getVGHealth(preflightVM.volumeGroupName, healthData)
      : { status: 'Unknown' as ReplicationHealthStatus, rpoSeconds: null };

    return {
      name: (
        <VMNodeContent
          vmName={vm.name}
          namespace={vm.namespace}
          consistencyLevel={preflightVM?.consistencyLevel === 'namespace' ? 'namespace' : 'vm'}
          storageBackend={backend}
          health={vgHealth}
        />
      ),
      id: `vm-discovered-${vm.namespace}-${vm.name}`,
    };
  });
}

function buildDRGroupChunks(
  groups: VolumeGroupInfo[],
  maxConcurrent: number,
  plan: DRPlan,
  healthData: VolumeGroupHealth[],
): TreeViewDataItem[] {
  if (!groups.length) return [];
  const chunkSize = maxConcurrent || groups.length;
  const chunks: TreeViewDataItem[] = [];

  for (let i = 0; i < groups.length; i += chunkSize) {
    const chunk = groups.slice(i, i + chunkSize);
    const chunkNum = Math.floor(i / chunkSize) + 1;
    chunks.push({
      name: (
        <span>DRGroup chunk {chunkNum} (maxConcurrent: {chunkSize})</span>
      ),
      id: `chunk-${i}`,
      children: chunk.flatMap((g) => buildVMNodes(g, plan, healthData)),
      defaultExpanded: true,
    });
  }
  return chunks;
}

interface WaveCompositionTreeProps {
  plan: DRPlan;
}

export const WaveCompositionTree: React.FC<WaveCompositionTreeProps> = ({ plan }) => {
  const healthData = plan.status?.replicationHealth ?? [];
  const maxConcurrent = plan.spec?.maxConcurrentFailovers ?? 0;

  const waveItems: TreeViewDataItem[] = useMemo(() => {
    const waves = plan.status?.waves ?? [];
    return waves.map((wave, idx) => {
      const groups = wave.groups ?? [];
      const vmCount = groups.reduce((sum, g) => sum + (g.vmNames?.length ?? 0), 0) || wave.vms?.length || 0;
      const children = groups.length > 0
        ? buildDRGroupChunks(groups, maxConcurrent, plan, healthData)
        : buildDiscoveredVMNodes(wave.vms ?? [], plan, healthData);
      const aggHealth = groups.length > 0 ? getAggregateHealth(groups, healthData) : null;
      const waveLabel = `Wave ${idx + 1}, ${vmCount} VMs${aggHealth ? `, replication ${aggHealth.toLowerCase()}` : ''}`;

      return {
        name: (
          <span
            style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--pf-t--global--spacer--sm)' }}
            aria-label={waveLabel}
          >
            Wave {idx + 1} — {vmCount} VMs
            {groups.length > 0 && (
              <AggregateHealthBadge groups={groups} healthData={healthData} />
            )}
          </span>
        ),
        id: `wave-${idx}`,
        children,
        defaultExpanded: false,
      };
    });
  }, [plan, healthData, maxConcurrent]);

  if (waveItems.length === 0) {
    return (
      <div style={{ padding: 'var(--pf-t--global--spacer--lg)', color: 'var(--pf-t--global--text--color--subtle)' }}>
        No waves configured for this plan
      </div>
    );
  }

  return <TreeView data={waveItems} aria-label="Wave composition" />;
};
