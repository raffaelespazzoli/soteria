import { useMemo } from 'react';
import { Alert, Content, ContentVariants } from '@patternfly/react-core';
import { ExclamationTriangleIcon } from '@patternfly/react-icons';
import { Table, Thead, Tr, Th, Tbody, Td } from '@patternfly/react-table';
import { DRPlan, DiscoveredVM } from '../../models/types';
import { formatRelativeTime } from '../../utils/formatters';

const STALE_THRESHOLD_MS = 5 * 60 * 1000;

function vmKey(vm: DiscoveredVM): string {
  return `${vm.namespace}/${vm.name}`;
}

function isStale(lastDiscoveryTime: string | undefined): boolean {
  if (!lastDiscoveryTime) return false;
  return Date.now() - new Date(lastDiscoveryTime).getTime() > STALE_THRESHOLD_MS;
}

interface SiteDiscoverySectionProps {
  plan: DRPlan;
}

export const SiteDiscoverySection: React.FC<SiteDiscoverySectionProps> = ({ plan }) => {
  const primary = plan.status?.primarySiteDiscovery;
  const secondary = plan.status?.secondarySiteDiscovery;
  const primarySiteName = plan.spec?.primarySite ?? 'Primary';
  const secondarySiteName = plan.spec?.secondarySite ?? 'Secondary';

  const { primaryOnlyKeys, secondaryOnlyKeys } = useMemo(() => {
    const pVMs = primary?.vms ?? [];
    const sVMs = secondary?.vms ?? [];
    const pKeys = new Set(pVMs.map(vmKey));
    const sKeys = new Set(sVMs.map(vmKey));
    return {
      primaryOnlyKeys: new Set([...pKeys].filter((k) => !sKeys.has(k))),
      secondaryOnlyKeys: new Set([...sKeys].filter((k) => !pKeys.has(k))),
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
          discovery={primary}
          mismatchKeys={primaryOnlyKeys}
          mismatchLabel="VM present on primary site only"
        />
        {/* Secondary site column */}
        <SiteColumn
          siteName={secondarySiteName}
          discovery={secondary}
          mismatchKeys={secondaryOnlyKeys}
          mismatchLabel="VM present on secondary site only"
        />
      </div>
    </div>
  );
};

interface SiteColumnProps {
  siteName: string;
  discovery: { vms?: DiscoveredVM[]; discoveredVMCount?: number; lastDiscoveryTime?: string } | undefined;
  mismatchKeys: Set<string>;
  mismatchLabel: string;
}

function SiteColumn({ siteName, discovery, mismatchKeys, mismatchLabel }: SiteColumnProps) {
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
              <Th>Name</Th>
              <Th>Namespace</Th>
              <Th>Status</Th>
            </Tr>
          </Thead>
          <Tbody>
            {vms.map((vm) => {
              const key = vmKey(vm);
              const isMismatch = mismatchKeys.has(key);
              return (
                <Tr
                  key={key}
                  style={
                    isMismatch
                      ? {
                          background:
                            'var(--pf-t--global--color--status--warning--default, var(--pf-v5-global--warning-color--100))',
                        }
                      : undefined
                  }
                >
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
                  </Td>
                </Tr>
              );
            })}
          </Tbody>
        </Table>
      )}
    </div>
  );
}
