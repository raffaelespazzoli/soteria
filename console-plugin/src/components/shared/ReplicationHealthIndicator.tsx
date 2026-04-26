import {
  CheckCircleIcon,
  ExclamationTriangleIcon,
  ExclamationCircleIcon,
  QuestionCircleIcon,
  SyncAltIcon,
} from '@patternfly/react-icons';
import { ReplicationHealth, ReplicationHealthStatus } from '../../utils/drPlanUtils';
import { formatRPO } from '../../utils/formatters';

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
  const rpoText = formatRPO(health.rpoSeconds);
  const ariaLabel = rpoText
    ? `Replication ${label.toLowerCase()}, ${rpoText}`
    : `Replication ${label.toLowerCase()}`;

  return (
    <span
      style={{ display: 'inline-flex', alignItems: 'center', gap: '0.25rem', whiteSpace: 'nowrap' }}
      aria-label={ariaLabel}
      role="status"
    >
      <Icon style={{ color: colorVar }} />
      <span>{label}</span>
      {rpoText && <span>{rpoText}</span>}
    </span>
  );
};

export default ReplicationHealthIndicator;
