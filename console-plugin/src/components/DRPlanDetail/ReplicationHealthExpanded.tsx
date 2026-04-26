import { Content, ContentVariants } from '@patternfly/react-core';
import { Table, Thead, Tr, Th, Tbody, Td } from '@patternfly/react-table';
import ReplicationHealthIndicator from '../shared/ReplicationHealthIndicator';
import { DRPlan, VolumeGroupHealth } from '../../models/types';
import { ReplicationHealthStatus, getReplicationHealth } from '../../utils/drPlanUtils';
import { formatRPO, formatRelativeTime } from '../../utils/formatters';

function vgStatusToHealth(vg: VolumeGroupHealth): { status: ReplicationHealthStatus; rpoSeconds: number | null } {
  const rpoMatch = vg.estimatedRPO?.match(/^(\d+)/);
  const parsed = rpoMatch ? parseInt(rpoMatch[1], 10) : NaN;
  const rpoSeconds = isNaN(parsed) ? null : parsed;
  return { status: vg.health as ReplicationHealthStatus, rpoSeconds };
}

interface ReplicationHealthExpandedProps {
  plan: DRPlan;
}

export const ReplicationHealthExpanded: React.FC<ReplicationHealthExpandedProps> = ({ plan }) => {
  const overallHealth = getReplicationHealth(plan);
  const vgHealth = plan.status?.replicationHealth ?? [];

  if (vgHealth.length === 0) {
    return (
      <div>
        <ReplicationHealthIndicator health={overallHealth} />
        <Content component={ContentVariants.small} style={{ color: 'var(--pf-t--global--text--color--subtle)', marginTop: 'var(--pf-t--global--spacer--sm)' }}>
          Per-volume-group breakdown not available
        </Content>
      </div>
    );
  }

  return (
    <Table aria-label="Replication health by volume group" variant="compact">
      <Thead>
        <Tr>
          <Th>Volume Group</Th>
          <Th>Health</Th>
          <Th>RPO</Th>
          <Th>Last Checked</Th>
        </Tr>
      </Thead>
      <Tbody>
        {vgHealth.map((vg) => (
          <Tr key={vg.name}>
            <Td dataLabel="Volume Group">{vg.name}</Td>
            <Td dataLabel="Health">
              <ReplicationHealthIndicator health={vgStatusToHealth(vg)} />
            </Td>
            <Td dataLabel="RPO">
              {vg.estimatedRPO != null
                ? formatRPO(isNaN(parseInt(vg.estimatedRPO, 10)) ? null : parseInt(vg.estimatedRPO, 10))
                : 'N/A'}
            </Td>
            <Td dataLabel="Last Checked">
              {vg.lastChecked ? formatRelativeTime(vg.lastChecked) : 'N/A'}
            </Td>
          </Tr>
        ))}
      </Tbody>
    </Table>
  );
};
