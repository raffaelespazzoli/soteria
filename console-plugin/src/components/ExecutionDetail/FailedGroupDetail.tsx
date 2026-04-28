import {
  ExpandableSection,
  Button,
  Tooltip,
  Alert,
} from '@patternfly/react-core';
import { ExclamationCircleIcon } from '@patternfly/react-icons';
import { DRGroupExecutionStatus, StepStatus } from '../../models/types';

function getFailedStep(group: DRGroupExecutionStatus): string | null {
  if (!group.steps?.length) return null;
  const failedStep = group.steps.find((s: StepStatus) => s.status === 'Failed');
  if (failedStep) return failedStep.name;
  const lastStep = group.steps[group.steps.length - 1];
  return lastStep.status !== 'Completed' ? lastStep.name : null;
}

export interface FailedGroupDetailProps {
  group: DRGroupExecutionStatus;
  onRetry?: () => void;
  isRetryDisabled: boolean;
  retryTooltip?: string;
  retryError?: string | null;
  showRetryButton: boolean;
}

const FailedGroupDetail: React.FC<FailedGroupDetailProps> = ({
  group,
  onRetry,
  isRetryDisabled,
  retryTooltip,
  retryError,
  showRetryButton,
}) => {
  const failedStep = getFailedStep(group);
  const hasRetries = (group.retryCount ?? 0) > 0;

  const iconStyle: React.CSSProperties = {
    color: 'var(--pf-t--global--icon--color--status--danger--default, var(--pf-v5-global--danger-color--100))',
    marginRight: 'var(--pf-t--global--spacer--xs, var(--pf-v5-global--spacer--xs))',
  };

  const containerStyle: React.CSSProperties = {
    paddingLeft: 'var(--pf-t--global--spacer--md, var(--pf-v5-global--spacer--md))',
    fontSize: 'var(--pf-t--global--font--size--body--default, 14px)',
  };

  const detailLineStyle: React.CSSProperties = {
    marginBottom: 'var(--pf-t--global--spacer--xs, var(--pf-v5-global--spacer--xs))',
  };

  const retryButton = showRetryButton ? (
    isRetryDisabled && retryTooltip ? (
      <Tooltip content={retryTooltip}>
        <Button
          variant="primary"
          size="sm"
          isDisabled
          aria-label={`Retry ${group.name}`}
          style={{ marginTop: 'var(--pf-t--global--spacer--sm, var(--pf-v5-global--spacer--sm))' }}
        >
          Retry
        </Button>
      </Tooltip>
    ) : (
      <Button
        variant="primary"
        size="sm"
        onClick={onRetry}
        isDisabled={isRetryDisabled}
        aria-label={`Retry ${group.name}`}
        style={{ marginTop: 'var(--pf-t--global--spacer--sm, var(--pf-v5-global--spacer--sm))' }}
      >
        Retry
      </Button>
    )
  ) : null;

  return (
    <ExpandableSection
      toggleText={`Error details for ${group.name}`}
      isExpanded
      data-testid={`failed-detail-${group.name}`}
      style={{ marginTop: 'var(--pf-t--global--spacer--xs, var(--pf-v5-global--spacer--xs))' }}
    >
      <div style={containerStyle}>
        <div style={detailLineStyle}>
          <ExclamationCircleIcon style={iconStyle} />
          <strong>Error: </strong>
          <span>{group.error ?? 'Unknown error'}</span>
        </div>

        {failedStep && (
          <div style={detailLineStyle}>
            <strong>Failed step: </strong>
            <span>{failedStep}</span>
          </div>
        )}

        {group.vmNames && group.vmNames.length > 0 && (
          <div style={detailLineStyle}>
            <strong>Affected VMs: </strong>
            <span>{group.vmNames.join(', ')}</span>
          </div>
        )}

        {hasRetries && (
          <div style={detailLineStyle}>
            Previously retried {group.retryCount} {group.retryCount === 1 ? 'time' : 'times'}
          </div>
        )}

        {retryButton}

        {retryError && (
          <Alert
            variant="danger"
            isInline
            isPlain
            title="Retry rejected"
            style={{ marginTop: 'var(--pf-t--global--spacer--sm, var(--pf-v5-global--spacer--sm))' }}
          >
            {retryError}
          </Alert>
        )}
      </div>
    </ExpandableSection>
  );
};

export { getFailedStep };
export default FailedGroupDetail;
