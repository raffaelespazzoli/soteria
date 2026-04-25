import { useState } from 'react';
import {
  Dropdown,
  DropdownItem,
  DropdownList,
  MenuToggle,
  MenuToggleElement,
} from '@patternfly/react-core';
import { EllipsisVIcon } from '@patternfly/react-icons';
import { DRPlan } from '../../models/types';
import { DRAction, getValidActions } from '../../utils/drPlanActions';

interface DRPlanActionsProps {
  plan: DRPlan;
}

const DRPlanActions: React.FC<DRPlanActionsProps> = ({ plan }) => {
  const [isOpen, setIsOpen] = useState(false);
  const actions = getValidActions(plan);

  if (actions.length === 0) return null;

  const onActionClick = (action: DRAction) => {
    // eslint-disable-next-line no-console
    console.log('Action:', action.key, 'Plan:', plan.metadata?.name);
    setIsOpen(false);
  };

  return (
    <Dropdown
      isOpen={isOpen}
      onOpenChange={setIsOpen}
      toggle={(toggleRef: React.Ref<MenuToggleElement>) => (
        <MenuToggle
          ref={toggleRef}
          variant="plain"
          onClick={() => setIsOpen((prev) => !prev)}
          aria-label={`Actions for ${plan.metadata?.name}`}
        >
          <EllipsisVIcon />
        </MenuToggle>
      )}
    >
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
