import { Alert, AlertActionLink } from '@patternfly/react-core';
import { DRPlan } from '../../models/types';
import { getSitesInSync, parseSiteDiscoveryDelta } from '../../utils/drPlanUtils';

interface SiteDisagreementAlertProps {
  plan: DRPlan;
  onSwitchToConfig: () => void;
}

export const SiteDisagreementAlert: React.FC<SiteDisagreementAlertProps> = ({
  plan,
  onSwitchToConfig,
}) => {
  const sitesInSync = getSitesInSync(plan);

  if (sitesInSync.inSync) return null;

  const delta = parseSiteDiscoveryDelta(sitesInSync.message);
  const parts: string[] = [];
  const primaryTotal = delta.primaryOnly.length + delta.primaryMoreCount;
  const secondaryTotal = delta.secondaryOnly.length + delta.secondaryMoreCount;
  if (primaryTotal > 0) {
    parts.push(`${primaryTotal} VM${primaryTotal > 1 ? 's' : ''} on primary not found on secondary`);
  }
  if (secondaryTotal > 0) {
    parts.push(`${secondaryTotal} VM${secondaryTotal > 1 ? 's' : ''} on secondary not found on primary`);
  }
  const summary = parts.length > 0 ? parts.join(', ') : sitesInSync.message;

  return (
    <Alert
      variant="danger"
      isInline
      title="Sites do not agree on VM inventory — DR operations are blocked"
      actionLinks={
        <AlertActionLink onClick={onSwitchToConfig}>View site differences</AlertActionLink>
      }
    >
      {summary && <p>{summary}</p>}
    </Alert>
  );
};
