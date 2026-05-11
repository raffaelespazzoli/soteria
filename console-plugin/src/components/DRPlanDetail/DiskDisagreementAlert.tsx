import { Alert, AlertActionLink } from '@patternfly/react-core';
import { DRPlan } from '../../models/types';
import { getDisksConsistent } from '../../utils/drPlanUtils';

interface DiskDisagreementAlertProps {
  plan: DRPlan;
  onSwitchToConfig: () => void;
}

const TITLES: Record<string, string> = {
  DiskMismatch: 'Disk topology does not match across sites — DR operations are blocked',
  StorageClassMixed: 'Volume group storage classes are mixed — DR operations are blocked',
  WaitingForDiskDiscovery: 'Waiting for disk discovery from both sites',
};

export const DiskDisagreementAlert: React.FC<DiskDisagreementAlertProps> = ({
  plan,
  onSwitchToConfig,
}) => {
  const disksConsistent = getDisksConsistent(plan);

  if (disksConsistent.consistent) return null;

  const title =
    TITLES[disksConsistent.reason ?? ''] ??
    'Disk topology inconsistent — DR operations are blocked';
  const variant = disksConsistent.reason === 'WaitingForDiskDiscovery' ? 'info' : 'danger';

  return (
    <Alert
      variant={variant}
      isInline
      title={title}
      actionLinks={<AlertActionLink onClick={onSwitchToConfig}>View disk details</AlertActionLink>}
    >
      {disksConsistent.message && <p>{disksConsistent.message}</p>}
    </Alert>
  );
};
