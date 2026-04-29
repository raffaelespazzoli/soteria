import {
  CheckCircleIcon,
  ExclamationTriangleIcon,
  ExclamationCircleIcon,
  MinusCircleIcon,
  QuestionCircleIcon,
  SyncAltIcon,
} from '@patternfly/react-icons';
import { ReplicationHealth, ReplicationHealthStatus } from '../../utils/drPlanUtils';

interface HealthConfig {
  Icon: React.ComponentType<{ style?: React.CSSProperties }>;
  colorVar: string;
  label: string;
}

const HEALTH_CONFIG: Record<ReplicationHealthStatus, HealthConfig> = {
  Healthy: {
    Icon: CheckCircleIcon,
    colorVar: 'var(--pf-t--global--icon--color--status--success--default)',
    label: 'Healthy',
  },
  Degraded: {
    Icon: ExclamationTriangleIcon,
    colorVar: 'var(--pf-t--global--icon--color--status--warning--default)',
    label: 'Degraded',
  },
  Syncing: {
    Icon: SyncAltIcon,
    colorVar: 'var(--pf-t--global--icon--color--status--info--default)',
    label: 'Syncing',
  },
  NotReplicating: {
    Icon: MinusCircleIcon,
    colorVar: 'var(--pf-t--global--icon--color--disabled)',
    label: 'Not replicating',
  },
  Error: {
    Icon: ExclamationCircleIcon,
    colorVar: 'var(--pf-t--global--icon--color--status--danger--default)',
    label: 'Error',
  },
  Unknown: {
    Icon: QuestionCircleIcon,
    colorVar: 'var(--pf-t--global--icon--color--disabled)',
    label: 'Unknown',
  },
};

interface ReplicationHealthIndicatorProps {
  health: ReplicationHealth;
}

const ReplicationHealthIndicator: React.FC<ReplicationHealthIndicatorProps> = ({ health }) => {
  const config = HEALTH_CONFIG[health.status];
  const { Icon, colorVar, label } = config;

  return (
    <span
      style={{ display: 'inline-flex', alignItems: 'center', gap: '0.25rem', whiteSpace: 'nowrap' }}
      aria-label={`Replication ${label.toLowerCase()}`}
      role="status"
    >
      <Icon style={{ color: colorVar }} />
      <span>{label}</span>
    </span>
  );
};

export default ReplicationHealthIndicator;
