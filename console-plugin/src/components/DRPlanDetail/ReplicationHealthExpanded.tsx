import { Content, ContentVariants } from '@patternfly/react-core';
import { Table, Thead, Tr, Th, Tbody, Td } from '@patternfly/react-table';
import ReplicationHealthIndicator from '../shared/ReplicationHealthIndicator';
import { DRPlan, VolumeGroupHealth } from '../../models/types';
import { ReplicationHealthStatus, getReplicationHealth } from '../../utils/drPlanUtils';
import { formatRelativeTime } from '../../utils/formatters';

function vgStatusToHealth(vg: VolumeGroupHealth): { status: ReplicationHealthStatus } {
  return { status: vg.health as ReplicationHealthStatus };
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
            <Td dataLabel="Last Checked">
              {vg.lastChecked ? formatRelativeTime(vg.lastChecked) : 'N/A'}
            </Td>
          </Tr>
        ))}
      </Tbody>
    </Table>
  );
};
