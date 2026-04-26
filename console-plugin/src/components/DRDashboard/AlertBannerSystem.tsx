import { useMemo } from 'react';
import { Alert, AlertActionLink } from '@patternfly/react-core';
import { DRPlan } from '../../models/types';
import { getReplicationHealth } from '../../utils/drPlanUtils';

interface AlertBannerSystemProps {
  plans: DRPlan[];
  onFilterByHealth: (healthStatus: string) => void;
}

export default function AlertBannerSystem({ plans, onFilterByHealth }: AlertBannerSystemProps) {
  const errorCount = useMemo(
    () => plans.filter((p) => getReplicationHealth(p).status === 'Error').length,
    [plans],
  );

  const degradedCount = useMemo(
    () => plans.filter((p) => getReplicationHealth(p).status === 'Degraded').length,
    [plans],
  );

  return (
    <>
      {errorCount > 0 && (
        <Alert
          variant="danger"
          isInline
          title={`${errorCount} DR ${errorCount === 1 ? 'Plan' : 'Plans'} running UNPROTECTED \u2014 replication broken`}
          actionLinks={
            <AlertActionLink onClick={() => onFilterByHealth('Error')}>
              View affected plans
            </AlertActionLink>
          }
        />
      )}
      {degradedCount > 0 && (
        <Alert
          variant="warning"
          isInline
          title={`${degradedCount} ${degradedCount === 1 ? 'plan' : 'plans'} with degraded replication`}
          actionLinks={
            <AlertActionLink onClick={() => onFilterByHealth('Degraded')}>
              View affected plans
            </AlertActionLink>
          }
        />
      )}
    </>
  );
}
