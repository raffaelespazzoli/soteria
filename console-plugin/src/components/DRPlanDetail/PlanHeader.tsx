import { Flex, FlexItem } from '@patternfly/react-core';
import { DRPlan } from '../../models/types';
import { getEffectivePhase } from '../../utils/drPlanUtils';
import PhaseBadge from '../shared/PhaseBadge';

interface PlanHeaderProps {
  plan: DRPlan;
}

function getVMCount(plan: DRPlan): number {
  if (plan.status?.discoveredVMCount != null) return plan.status.discoveredVMCount;
  if (!plan.status?.waves) return 0;
  return plan.status.waves.reduce((sum, w) => sum + w.vms.length, 0);
}

function getWaveCount(plan: DRPlan): number {
  return plan.status?.waves?.length ?? 0;
}

const PlanHeader: React.FC<PlanHeaderProps> = ({ plan }) => {
  const effectivePhase = getEffectivePhase(plan);
  const vmCount = getVMCount(plan);
  const waveCount = getWaveCount(plan);
  const activeSite = plan.status?.activeSite;

  return (
    <div
      style={{
        padding: 'var(--pf-v5-global--spacer--md) 0',
        marginBottom: 'var(--pf-v5-global--spacer--md)',
      }}
    >
      <Flex alignItems={{ default: 'alignItemsCenter' }} spaceItems={{ default: 'spaceItemsMd' }}>
        <FlexItem>
          <span
            style={{
              fontSize: 'var(--pf-v5-global--FontSize--lg)',
              fontWeight: 'var(--pf-v5-global--FontWeight--bold)' as unknown as number,
            }}
          >
            {plan.metadata?.name}
          </span>
        </FlexItem>
        <FlexItem>
          <PhaseBadge phase={effectivePhase} />
        </FlexItem>
      </Flex>
      <Flex
        spaceItems={{ default: 'spaceItemsLg' }}
        style={{ marginTop: 'var(--pf-v5-global--spacer--sm)' }}
      >
        <FlexItem>
          <strong style={{ fontSize: 'var(--pf-v5-global--FontSize--lg)' }}>{vmCount}</strong> {vmCount === 1 ? 'VM' : 'VMs'}
        </FlexItem>
        <FlexItem>
          <strong style={{ fontSize: 'var(--pf-v5-global--FontSize--lg)' }}>{waveCount}</strong> {waveCount === 1 ? 'wave' : 'waves'}
        </FlexItem>
        {activeSite && <FlexItem>Active on: {activeSite}</FlexItem>}
      </Flex>
    </div>
  );
};

export default PlanHeader;
