import { useState } from 'react';
import {
  Dropdown,
  DropdownItem,
  DropdownList,
  MenuToggle,
  MenuToggleElement,
  Tooltip,
} from '@patternfly/react-core';
import { EllipsisVIcon } from '@patternfly/react-icons';
import { DRPlan } from '../../models/types';
import { DRAction, getValidActions } from '../../utils/drPlanActions';
import { EffectivePhase } from '../../utils/drPlanUtils';

interface DRPlanActionsProps {
  plan: DRPlan;
  effectivePhase: EffectivePhase;
  onAction?: (actionKey: string, plan: DRPlan) => void;
  isDisabled?: boolean;
  disabledTooltip?: string;
}

const DRPlanActions: React.FC<DRPlanActionsProps> = ({
  plan,
  effectivePhase,
  onAction,
  isDisabled,
  disabledTooltip,
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const actions = getValidActions(effectivePhase);

  if (actions.length === 0) return null;

  const onActionClick = (action: DRAction) => {
    if (onAction) {
      onAction(action.key, plan);
    }
    setIsOpen(false);
  };

  const toggle = (toggleRef: React.Ref<MenuToggleElement>) => (
    <MenuToggle
      ref={toggleRef}
      variant="plain"
      onClick={() => !isDisabled && setIsOpen((prev) => !prev)}
      aria-label={`Actions for ${plan.metadata?.name}`}
      isDisabled={isDisabled}
    >
      <EllipsisVIcon />
    </MenuToggle>
  );

  if (isDisabled && disabledTooltip) {
    return (
      <Tooltip content={disabledTooltip}>
        <span style={{ display: 'inline-block' }}>
          <Dropdown isOpen={false} onOpenChange={() => {}} toggle={toggle}>
            <DropdownList>{null}</DropdownList>
          </Dropdown>
        </span>
      </Tooltip>
    );
  }

  return (
    <Dropdown isOpen={isOpen} onOpenChange={setIsOpen} toggle={toggle}>
      <DropdownList>
        {actions.map((action) => (
          <DropdownItem
            key={action.key}
            onClick={() => onActionClick(action)}
            isDanger={action.isDanger}
          >
            {action.label}
          </DropdownItem>
        ))}
      </DropdownList>
    </Dropdown>
  );
};

export default DRPlanActions;
