import * as React from 'react';
import { useMemo, useState, useCallback } from 'react';
import { Alert, Content, ContentVariants } from '@patternfly/react-core';
import { ExclamationTriangleIcon, AngleDownIcon, AngleRightIcon } from '@patternfly/react-icons';
import { Table, Thead, Tr, Th, Tbody, Td } from '@patternfly/react-table';
import { DRPlan, DiscoveredVM, DiscoveredDisk } from '../../models/types';
import { formatRelativeTime } from '../../utils/formatters';

const STALE_THRESHOLD_MS = 5 * 60 * 1000;

function vmKey(vm: DiscoveredVM): string {
  return `${vm.namespace}/${vm.name}`;
}

function isStale(lastDiscoveryTime: string | undefined): boolean {
  if (!lastDiscoveryTime) return false;
  return Date.now() - new Date(lastDiscoveryTime).getTime() > STALE_THRESHOLD_MS;
}

interface DiskComparison {
  name: string;
  localDisk: DiscoveredDisk;
  partnerDisk: DiscoveredDisk;
  storageClassDiffers: boolean;
}

interface DiskComparisonResult {
  matches: DiskComparison[];
  localOnly: DiscoveredDisk[];
  partnerOnly: DiscoveredDisk[];
}

function compareDisksBySite(
  localDisks: DiscoveredDisk[] | undefined,
  partnerDisks: DiscoveredDisk[] | undefined,
): DiskComparisonResult {
  const local = localDisks ?? [];
  const partner = partnerDisks ?? [];
  const partnerByName = new Map(partner.map((d) => [d.name, d]));
  const localByName = new Map(local.map((d) => [d.name, d]));

  const matches: DiskComparison[] = [];
  const localOnly: DiscoveredDisk[] = [];

  for (const disk of local) {
    const match = partnerByName.get(disk.name);
    if (match) {
      matches.push({
        name: disk.name,
        localDisk: disk,
        partnerDisk: match,
        storageClassDiffers: disk.storageClass !== match.storageClass,
      });
    } else {
      localOnly.push(disk);
    }
  }

  const partnerOnly = partner.filter((d) => !localByName.has(d.name));

  return { matches, localOnly, partnerOnly };
}

interface SiteDiscoverySectionProps {
  plan: DRPlan;
}

export const SiteDiscoverySection: React.FC<SiteDiscoverySectionProps> = ({ plan }) => {
  const primary = plan.status?.primarySiteDiscovery;
  const secondary = plan.status?.secondarySiteDiscovery;
  const primarySiteName = plan.spec?.primarySite ?? 'Primary';
  const secondarySiteName = plan.spec?.secondarySite ?? 'Secondary';

  const { primaryOnlyKeys, secondaryOnlyKeys, partnerDiskIndex } = useMemo(() => {
    const pVMs = primary?.vms ?? [];
    const sVMs = secondary?.vms ?? [];
    const pKeys = new Set(pVMs.map(vmKey));
    const sKeys = new Set(sVMs.map(vmKey));
    const diskIdx = new Map<string, DiscoveredDisk[]>();
    for (const vm of pVMs) diskIdx.set(`primary:${vmKey(vm)}`, vm.disks ?? []);
    for (const vm of sVMs) diskIdx.set(`secondary:${vmKey(vm)}`, vm.disks ?? []);
    return {
      primaryOnlyKeys: new Set([...pKeys].filter((k) => !sKeys.has(k))),
      secondaryOnlyKeys: new Set([...sKeys].filter((k) => !pKeys.has(k))),
      partnerDiskIndex: diskIdx,
    };
  }, [primary, secondary]);

  if (!primary && !secondary) {
    return (
      <div id="site-discovery-section">
        <Content component={ContentVariants.h3}>Site Discovery</Content>
        <p>
          Site discovery not yet available. Ensure both Soteria instances are running with
          --site-name.
        </p>
      </div>
    );
  }

  return (
    <div id="site-discovery-section">
      <Content component={ContentVariants.h3}>Site Discovery</Content>
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: '1fr 1fr',
          gap: 'var(--pf-t--global--spacer--lg, var(--pf-v5-global--spacer--lg))',
        }}
      >
        {/* Primary site column */}
        <SiteColumn
          siteName={primarySiteName}
          siteKey="primary"
          partnerKey="secondary"
          discovery={primary}
          mismatchKeys={primaryOnlyKeys}
          mismatchLabel="VM present on primary site only"
          partnerDiskIndex={partnerDiskIndex}
        />
        {/* Secondary site column */}
        <SiteColumn
          siteName={secondarySiteName}
          siteKey="secondary"
          partnerKey="primary"
          discovery={secondary}
          mismatchKeys={secondaryOnlyKeys}
          mismatchLabel="VM present on secondary site only"
          partnerDiskIndex={partnerDiskIndex}
        />
      </div>
    </div>
  );
};

interface SiteColumnProps {
  siteName: string;
  siteKey: string;
  partnerKey: string;
  discovery:
    | { vms?: DiscoveredVM[]; discoveredVMCount?: number; lastDiscoveryTime?: string }
    | undefined;
  mismatchKeys: Set<string>;
  mismatchLabel: string;
  partnerDiskIndex: Map<string, DiscoveredDisk[]>;
}

function SiteColumn({
  siteName,
  siteKey: _siteKey,
  partnerKey,
  discovery,
  mismatchKeys,
  mismatchLabel,
  partnerDiskIndex,
}: SiteColumnProps) {
  const [expandedVMs, setExpandedVMs] = useState<Set<string>>(new Set());

  const toggleExpand = useCallback((key: string) => {
    setExpandedVMs((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

  if (!discovery) {
    return (
      <div>
        <Content component={ContentVariants.h4}>{siteName}</Content>
        <p>Waiting for {siteName} to report discovery data</p>
      </div>
    );
  }

  const vms = discovery.vms ?? [];
  const stale = isStale(discovery.lastDiscoveryTime);

  return (
    <div>
      <Content component={ContentVariants.h4}>{siteName}</Content>
      <p>
        {discovery.discoveredVMCount ?? vms.length} VMs discovered
        {discovery.lastDiscoveryTime && (
          <> &mdash; last updated {formatRelativeTime(discovery.lastDiscoveryTime)}</>
        )}
      </p>
      {stale && (
        <Alert
          variant="warning"
          isInline
          isPlain
          title={`Discovery data from ${siteName} is stale (last updated ${formatRelativeTime(discovery.lastDiscoveryTime)})`}
        />
      )}
      {vms.length > 0 && (
        <Table aria-label={`${siteName} discovered VMs`} variant="compact">
          <Thead>
            <Tr>
              <Th screenReaderText="Expand" />
              <Th>Name</Th>
              <Th>Namespace</Th>
              <Th>Status</Th>
            </Tr>
          </Thead>
          <Tbody>
            {vms.map((vm) => {
              const key = vmKey(vm);
              const isMismatch = mismatchKeys.has(key);
              const hasDisks = (vm.disks?.length ?? 0) > 0;
              const isExpanded = expandedVMs.has(key);
              const partnerDisks = partnerDiskIndex.get(`${partnerKey}:${key}`);
              const comparison = hasDisks ? compareDisksBySite(vm.disks, partnerDisks) : null;

              return (
                <React.Fragment key={key}>
                  <Tr
                    style={
                      isMismatch
                        ? {
                            background:
                              'var(--pf-t--global--color--status--warning--default, var(--pf-v5-global--warning-color--100))',
                          }
                        : undefined
                    }
                  >
                    <Td
                      dataLabel="Expand"
                      style={{ width: '40px', cursor: hasDisks ? 'pointer' : 'default' }}
                    >
                      {hasDisks ? (
                        <button
                          aria-expanded={isExpanded}
                          aria-label={`Show disks for ${vm.name}`}
                          onClick={() => toggleExpand(key)}
                          style={{
                            background: 'none',
                            border: 'none',
                            cursor: 'pointer',
                            padding: 0,
                          }}
                        >
                          {isExpanded ? <AngleDownIcon /> : <AngleRightIcon />}
                        </button>
                      ) : null}
                    </Td>
                    <Td dataLabel="Name">{vm.name}</Td>
                    <Td dataLabel="Namespace">{vm.namespace}</Td>
                    <Td dataLabel="Status">
                      {isMismatch && (
                        <span>
                          <ExclamationTriangleIcon
                            color="var(--pf-t--global--icon--color--status--warning--default, var(--pf-v5-global--warning-color--100))"
                            aria-hidden="true"
                          />
                          <span className="pf-v5-u-screen-reader">{mismatchLabel}</span>
                        </span>
                      )}
                      {!hasDisks && (
                        <span
                          style={{
                            color:
                              'var(--pf-t--global--text--color--subtle, var(--pf-v5-global--Color--200))',
                          }}
                        >
                          No PVC disks
                        </span>
                      )}
                    </Td>
                  </Tr>
                  {isExpanded && (
                    <Tr>
                      <Td
                        colSpan={4}
                        style={{
                          paddingLeft:
                            'var(--pf-t--global--spacer--xl, var(--pf-v5-global--spacer--xl))',
                        }}
                      >
                        {hasDisks && comparison ? (
                          <DiskDetailTable
                            comparison={comparison}
                            siteName={siteName}
                            vmName={vm.name}
                          />
                        ) : (
                          <span
                            style={{
                              color:
                                'var(--pf-t--global--text--color--subtle, var(--pf-v5-global--Color--200))',
                            }}
                          >
                            No PVC disks
                          </span>
                        )}
                      </Td>
                    </Tr>
                  )}
                </React.Fragment>
              );
            })}
          </Tbody>
        </Table>
      )}
    </div>
  );
}

function DiskDetailTable({
  comparison,
  siteName,
  vmName,
}: {
  comparison: DiskComparisonResult;
  siteName: string;
  vmName: string;
}) {
  const allRows = [
    ...comparison.matches.map((m) => ({
      name: m.localDisk.name,
      pvcName: m.localDisk.pvcName,
      storageClass: m.localDisk.storageClass,
      status: m.storageClassDiffers ? ('sc-mismatch' as const) : ('match' as const),
    })),
    ...comparison.localOnly.map((d) => ({
      name: d.name,
      pvcName: d.pvcName,
      storageClass: d.storageClass,
      status: 'local-only' as const,
    })),
    ...comparison.partnerOnly.map((d) => ({
      name: d.name,
      pvcName: d.pvcName,
      storageClass: d.storageClass,
      status: 'partner-only' as const,
    })),
  ];

  if (allRows.length === 0) {
    return (
      <span
        style={{
          color: 'var(--pf-t--global--text--color--subtle, var(--pf-v5-global--Color--200))',
        }}
      >
        No PVC disks
      </span>
    );
  }

  return (
    <Table aria-label={`${siteName} disks for ${vmName}`} variant="compact">
      <Thead>
        <Tr>
          <Th>Disk Name</Th>
          <Th>PVC Name</Th>
          <Th>Storage Class</Th>
          <Th>Comparison</Th>
        </Tr>
      </Thead>
      <Tbody>
        {allRows.map((row) => {
          const bgStyle =
            row.status === 'sc-mismatch'
              ? {
                  background:
                    'var(--pf-t--global--color--status--danger--default, var(--pf-v5-global--danger-color--100))',
                }
              : row.status === 'local-only' || row.status === 'partner-only'
                ? {
                    background:
                      'var(--pf-t--global--color--status--warning--default, var(--pf-v5-global--warning-color--100))',
                  }
                : undefined;
          return (
            <Tr key={row.name} style={bgStyle}>
              <Td dataLabel="Disk Name">{row.name}</Td>
              <Td dataLabel="PVC Name">{row.pvcName}</Td>
              <Td dataLabel="Storage Class">{row.storageClass}</Td>
              <Td dataLabel="Comparison">
                {row.status === 'match' ? null : (
                  <span>
                    <ExclamationTriangleIcon
                      color={
                        row.status === 'sc-mismatch'
                          ? 'var(--pf-t--global--icon--color--status--danger--default, var(--pf-v5-global--danger-color--100))'
                          : 'var(--pf-t--global--icon--color--status--warning--default, var(--pf-v5-global--warning-color--100))'
                      }
                      aria-hidden="true"
                    />
                    <span className="pf-v5-u-screen-reader">
                      {row.status === 'sc-mismatch'
                        ? 'Storage class differs from partner site'
                        : row.status === 'local-only'
                          ? 'Disk missing on partner site'
                          : 'Disk only on partner site'}
                    </span>
                  </span>
                )}
              </Td>
            </Tr>
          );
        })}
      </Tbody>
    </Table>
  );
}
