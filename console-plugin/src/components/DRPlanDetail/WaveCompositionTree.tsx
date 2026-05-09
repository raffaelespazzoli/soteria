import { useMemo } from 'react';
import { TreeView, TreeViewDataItem, Label } from '@patternfly/react-core';
import {
  CheckCircleIcon,
  ExclamationTriangleIcon,
  ExclamationCircleIcon,
  MinusCircleIcon,
  QuestionCircleIcon,
  SyncAltIcon,
  VirtualMachineIcon,
  StorageDomainIcon,
} from '@patternfly/react-icons';
import ReplicationHealthIndicator from '../shared/ReplicationHealthIndicator';
import { DRPlan, DiscoveredVM, VolumeGroupInfo, VolumeGroupHealth, PreflightVolumeGroup } from '../../models/types';
import { ReplicationHealthStatus } from '../../utils/drPlanUtils';

function getVGHealth(
  vgName: string,
  healthData: VolumeGroupHealth[],
): { status: ReplicationHealthStatus } {
  const vg = healthData.find((h) => h.name === vgName);
  if (!vg) return { status: 'Unknown' };
  return { status: vg.health as ReplicationHealthStatus };
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
  if (statuses.includes('NotReplicating')) return 'NotReplicating';
  return 'Healthy';
}

const HEALTH_LABEL_COLORS: Record<ReplicationHealthStatus, 'green' | 'yellow' | 'blue' | 'red' | 'grey'> = {
  Healthy: 'green',
  Degraded: 'yellow',
  Syncing: 'blue',
  NotReplicating: 'grey',
  Error: 'red',
  Unknown: 'grey',
};

const HEALTH_ICONS: Record<ReplicationHealthStatus, React.ReactElement> = {
  Healthy: <CheckCircleIcon />,
  Degraded: <ExclamationTriangleIcon />,
  Syncing: <SyncAltIcon />,
  NotReplicating: <MinusCircleIcon />,
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
  health: { status: ReplicationHealthStatus };
}) {
  const ariaStr = [
    vmName,
    storageBackend,
    consistencyLevel === 'namespace' ? `namespace ${namespace}` : 'VM-level consistency',
    `replication ${health.status.toLowerCase()}`,
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
      : { status: 'Unknown' as ReplicationHealthStatus };

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

interface ChunkVolumeGroups {
  enriched: PreflightVolumeGroup[];
  legacyNames: string[];
}

function getChunkVolumeGroups(chunk: { volumeGroups?: PreflightVolumeGroup[] }): ChunkVolumeGroups {
  const vgs = chunk.volumeGroups ?? [];
  if (vgs.length === 0) return { enriched: [], legacyNames: [] };
  if (typeof (vgs[0] as unknown) === 'string') {
    return { enriched: [], legacyNames: vgs as unknown as string[] };
  }
  return { enriched: vgs as PreflightVolumeGroup[], legacyNames: [] };
}

function buildVGDiskNodes(vg: PreflightVolumeGroup): TreeViewDataItem[] {
  if (!vg.disks?.length) return [];
  return vg.disks.map((disk) => ({
    name: (
      <span style={{ fontSize: 'var(--pf-t--global--font--size--body--sm, var(--pf-v5-global--FontSize--sm))' }}>
        {disk.name} → {disk.pvcName ?? 'N/A'} ({disk.pvcNamespace ?? 'N/A'})
      </span>
    ),
    id: `vg-disk-${vg.name}-${disk.name}`,
  }));
}

function buildLegacyVGNodes(names: string[], waveKey: string): TreeViewDataItem[] {
  return names.map((name) => ({
    name: (
      <span style={{ fontWeight: 600 }}>{name}</span>
    ),
    id: `vg-legacy-${waveKey}-${name}`,
  }));
}

function buildVGNodes(
  preflightVGs: PreflightVolumeGroup[],
  waveKey: string,
): TreeViewDataItem[] {
  if (preflightVGs.length === 0) return [];
  return preflightVGs.map((vg) => ({
    name: (
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--pf-t--global--spacer--sm)' }}>
        <span style={{ fontWeight: 600 }}>{vg.name}</span>
        {vg.site && (
          <Label isCompact color="blue">{vg.site}</Label>
        )}
        <span style={{ fontSize: 'var(--pf-t--global--font--size--body--sm, var(--pf-v5-global--FontSize--sm))', color: 'var(--pf-t--global--text--color--subtle)' }}>
          {vg.disks?.length ?? 0} disk{(vg.disks?.length ?? 0) !== 1 ? 's' : ''}
        </span>
      </span>
    ),
    id: `vg-${waveKey}-${vg.name}-${vg.site ?? ''}`,
    children: buildVGDiskNodes(vg),
    defaultExpanded: false,
  }));
}

function buildDRGroupChunks(
  groups: VolumeGroupInfo[],
  maxConcurrent: number,
  plan: DRPlan,
  healthData: VolumeGroupHealth[],
  waveKey?: string,
): TreeViewDataItem[] {
  if (!groups.length) return [];
  const chunkSize = maxConcurrent || groups.length;
  const chunks: TreeViewDataItem[] = [];

  const preflightWave = waveKey
    ? plan.status?.preflight?.waves?.find((w) => w.waveKey === waveKey)
    : undefined;
  const preflightChunks = preflightWave?.chunks ?? [];

  for (let i = 0; i < groups.length; i += chunkSize) {
    const chunk = groups.slice(i, i + chunkSize);
    const chunkNum = Math.floor(i / chunkSize) + 1;
    const vmNodes = chunk.flatMap((g) => buildVMNodes(g, plan, healthData));

    const pfChunk = preflightChunks[Math.floor(i / chunkSize)];
    const chunkVGs = pfChunk ? getChunkVolumeGroups(pfChunk) : { enriched: [], legacyNames: [] };
    const chunkVGNodes = [
      ...buildVGNodes(chunkVGs.enriched, waveKey ?? ''),
      ...buildLegacyVGNodes(chunkVGs.legacyNames, waveKey ?? ''),
    ];

    const children: TreeViewDataItem[] = [];
    if (vmNodes.length > 0) {
      children.push({
        name: (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--pf-t--global--spacer--sm)' }}>
            <VirtualMachineIcon />
            <span style={{ fontWeight: 600 }}>Virtual Machines ({vmNodes.length})</span>
          </span>
        ),
        id: `chunk-${i}-vms`,
        children: vmNodes,
        defaultExpanded: true,
      });
    }
    if (chunkVGNodes.length > 0) {
      children.push({
        name: (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--pf-t--global--spacer--sm)' }}>
            <StorageDomainIcon />
            <span style={{ fontWeight: 600 }}>Volume Groups ({chunkVGNodes.length})</span>
          </span>
        ),
        id: `chunk-${i}-vgs`,
        children: chunkVGNodes,
        defaultExpanded: false,
      });
    }

    chunks.push({
      name: (
        <span>DRGroup chunk {chunkNum} (maxConcurrent: {chunkSize})</span>
      ),
      id: `chunk-${i}`,
      children,
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
        ? buildDRGroupChunks(groups, maxConcurrent, plan, healthData, wave.waveKey)
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
