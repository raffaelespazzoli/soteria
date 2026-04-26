import { Label, LabelProps, Spinner } from '@patternfly/react-core';
import { CheckCircleIcon, InfoCircleIcon } from '@patternfly/react-icons';
import { EffectivePhase } from '../../utils/drPlanUtils';
import { isTransientPhase } from '../../utils/drPlanActions';

const VISUALLY_HIDDEN: React.CSSProperties = {
  position: 'absolute',
  width: '1px',
  height: '1px',
  padding: 0,
  margin: '-1px',
  overflow: 'hidden',
  clip: 'rect(0,0,0,0)',
  whiteSpace: 'nowrap',
  borderWidth: 0,
};

interface PhaseConfig {
  status?: LabelProps['status'];
  color?: LabelProps['color'];
  variant?: LabelProps['variant'];
  icon: React.ReactNode;
  label: string;
}

const PHASE_DISPLAY: Record<EffectivePhase, PhaseConfig> = {
  SteadyState: {
    status: 'success',
    variant: 'filled',
    icon: <CheckCircleIcon />,
    label: 'Steady State',
  },
  DRedSteadyState: {
    status: 'success',
    variant: 'filled',
    icon: <CheckCircleIcon />,
    label: 'DR Steady State',
  },
  FailedOver: {
    color: 'blue',
    variant: 'filled',
    icon: <InfoCircleIcon />,
    label: 'Failed Over',
  },
  FailedBack: {
    color: 'blue',
    variant: 'filled',
    icon: <InfoCircleIcon />,
    label: 'Failed Back',
  },
  FailingOver: {
    color: 'blue',
    variant: 'outline',
    icon: <Spinner size="sm" aria-label="Failing over" />,
    label: 'Failing Over',
  },
  Reprotecting: {
    color: 'blue',
    variant: 'outline',
    icon: <Spinner size="sm" aria-label="Reprotecting" />,
    label: 'Reprotecting',
  },
  FailingBack: {
    color: 'blue',
    variant: 'outline',
    icon: <Spinner size="sm" aria-label="Failing back" />,
    label: 'Failing Back',
  },
  Restoring: {
    color: 'blue',
    variant: 'outline',
    icon: <Spinner size="sm" aria-label="Restoring" />,
    label: 'Restoring',
  },
};

interface PhaseBadgeProps {
  phase: EffectivePhase;
}

const PhaseBadge: React.FC<PhaseBadgeProps> = ({ phase }) => {
  const config = PHASE_DISPLAY[phase] ?? PHASE_DISPLAY.SteadyState;
  const transient = isTransientPhase(phase);

  return (
    <Label
      status={config.status}
      color={config.color}
      variant={config.variant}
      icon={config.icon}
      isCompact
    >
      {config.label}
      {transient && <span style={VISUALLY_HIDDEN}> (in progress)</span>}
    </Label>
  );
};

export default PhaseBadge;
