import { Label, LabelProps } from '@patternfly/react-core';
import {
  CheckCircleIcon,
  ExclamationTriangleIcon,
  ExclamationCircleIcon,
} from '@patternfly/react-icons';
import { DRExecutionResult } from '../../models/types';

interface ResultConfig {
  status: NonNullable<LabelProps['status']>;
  icon: React.ReactNode;
  label: string;
}

const RESULT_DISPLAY: Record<DRExecutionResult, ResultConfig> = {
  Succeeded: {
    status: 'success',
    icon: <CheckCircleIcon />,
    label: 'Succeeded',
  },
  PartiallySucceeded: {
    status: 'warning',
    icon: <ExclamationTriangleIcon />,
    label: 'Partial',
  },
  Failed: {
    status: 'danger',
    icon: <ExclamationCircleIcon />,
    label: 'Failed',
  },
};

interface ExecutionResultBadgeProps {
  result: DRExecutionResult;
}

const ExecutionResultBadge: React.FC<ExecutionResultBadgeProps> = ({ result }) => {
  const config = RESULT_DISPLAY[result] ?? RESULT_DISPLAY.Failed;
  return (
    <Label status={config.status} icon={config.icon} isCompact>
      {config.label}
    </Label>
  );
};

export default ExecutionResultBadge;
