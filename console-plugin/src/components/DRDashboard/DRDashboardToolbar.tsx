import { useCallback, useMemo, useRef, useState } from 'react';
import {
  MenuToggle,
  MenuToggleElement,
  SearchInput,
  Select,
  SelectList,
  SelectOption,
  Toolbar,
  ToolbarContent,
  ToolbarFilter,
  ToolbarGroup,
  ToolbarItem,
} from '@patternfly/react-core';
import { DRPlan } from '../../models/types';
import { FilterState } from '../../hooks/useFilterParams';

const PHASE_OPTIONS = [
  'SteadyState',
  'FailedOver',
  'FailingOver',
  'Reprotecting',
  'DRedSteadyState',
  'FailingBack',
  'FailedBack',
  'Restoring',
];

const PROTECTED_OPTIONS = ['Healthy', 'Degraded', 'Error', 'Unknown'];

const LAST_EXECUTION_OPTIONS = ['Succeeded', 'PartiallySucceeded', 'Failed', 'InProgress', 'Never'];

interface DRDashboardToolbarProps {
  filters: FilterState;
  onFiltersChange: (filters: FilterState) => void;
  onClearAll: () => void;
  plans: DRPlan[];
  filteredCount: number;
  totalCount: number;
}

type FilterKey = 'phase' | 'activeOn' | 'protected' | 'lastExecution';

function MultiSelectFilter({
  categoryName,
  filterKey,
  options,
  selected,
  onToggle,
}: {
  categoryName: string;
  filterKey: string;
  options: string[];
  selected: string[];
  onToggle: (value: string) => void;
}) {
  const [isOpen, setIsOpen] = useState(false);
  const toggleRef = useRef<MenuToggleElement>(null);

  return (
    <Select
      role="menu"
      isOpen={isOpen}
      selected={selected}
      onSelect={(_event, value) => {
        if (typeof value === 'string') onToggle(value);
      }}
      onOpenChange={setIsOpen}
      toggle={(ref: React.Ref<MenuToggleElement>) => {
        (toggleRef as React.MutableRefObject<MenuToggleElement | null>).current = (
          ref as React.RefObject<MenuToggleElement>
        ).current;
        return (
          <MenuToggle ref={ref} onClick={() => setIsOpen((prev) => !prev)} isExpanded={isOpen}>
            {categoryName}
            {selected.length > 0 && ` (${selected.length})`}
          </MenuToggle>
        );
      }}
    >
      <SelectList>
        {options.map((opt) => (
          <SelectOption
            key={`${filterKey}-${opt}`}
            value={opt}
            hasCheckbox
            isSelected={selected.includes(opt)}
          >
            {opt}
          </SelectOption>
        ))}
      </SelectList>
    </Select>
  );
}

const DRDashboardToolbar: React.FC<DRDashboardToolbarProps> = ({
  filters,
  onFiltersChange,
  onClearAll,
  plans,
  filteredCount,
  totalCount,
}) => {
  const activeOnOptions = useMemo(() => {
    const sites = new Set<string>();
    plans.forEach((p) => {
      if (p.status?.activeSite) sites.add(p.status.activeSite);
    });
    return Array.from(sites).sort();
  }, [plans]);

  const updateFilter = useCallback(
    (key: FilterKey, values: string[]) => {
      onFiltersChange({ ...filters, [key]: values });
    },
    [filters, onFiltersChange],
  );

  const toggleFilterValue = useCallback(
    (key: FilterKey, value: string) => {
      const current = filters[key];
      const next = current.includes(value)
        ? current.filter((v) => v !== value)
        : [...current, value];
      updateFilter(key, next);
    },
    [filters, updateFilter],
  );

  const deleteLabel = useCallback(
    (key: FilterKey, label: string) => {
      updateFilter(
        key,
        filters[key].filter((v) => v !== label),
      );
    },
    [filters, updateFilter],
  );

  return (
    <Toolbar clearAllFilters={onClearAll}>
      <ToolbarContent>
        <ToolbarItem>
          <SearchInput
            placeholder="Filter by name..."
            value={filters.search}
            onChange={(_event, value) => onFiltersChange({ ...filters, search: value })}
            onClear={() => onFiltersChange({ ...filters, search: '' })}
            aria-label="Filter plans by name"
          />
        </ToolbarItem>

        <ToolbarGroup variant="filter-group">
          <ToolbarFilter
            labels={filters.phase}
            deleteLabel={(_category, label) => deleteLabel('phase', label as string)}
            categoryName="Phase"
          >
            <MultiSelectFilter
              categoryName="Phase"
              filterKey="phase"
              options={PHASE_OPTIONS}
              selected={filters.phase}
              onToggle={(v) => toggleFilterValue('phase', v)}
            />
          </ToolbarFilter>

          <ToolbarFilter
            labels={filters.activeOn}
            deleteLabel={(_category, label) => deleteLabel('activeOn', label as string)}
            categoryName="Active On"
          >
            <MultiSelectFilter
              categoryName="Active On"
              filterKey="activeOn"
              options={activeOnOptions}
              selected={filters.activeOn}
              onToggle={(v) => toggleFilterValue('activeOn', v)}
            />
          </ToolbarFilter>

          <ToolbarFilter
            labels={filters.protected}
            deleteLabel={(_category, label) => deleteLabel('protected', label as string)}
            categoryName="Protected"
          >
            <MultiSelectFilter
              categoryName="Protected"
              filterKey="protected"
              options={PROTECTED_OPTIONS}
              selected={filters.protected}
              onToggle={(v) => toggleFilterValue('protected', v)}
            />
          </ToolbarFilter>

          <ToolbarFilter
            labels={filters.lastExecution}
            deleteLabel={(_category, label) => deleteLabel('lastExecution', label as string)}
            categoryName="Last Execution"
          >
            <MultiSelectFilter
              categoryName="Last Execution"
              filterKey="lastExecution"
              options={LAST_EXECUTION_OPTIONS}
              selected={filters.lastExecution}
              onToggle={(v) => toggleFilterValue('lastExecution', v)}
            />
          </ToolbarFilter>
        </ToolbarGroup>

        <ToolbarItem variant="pagination" align={{ default: 'alignEnd' }}>
          <span>
            Showing {filteredCount} of {totalCount} plans
          </span>
        </ToolbarItem>
      </ToolbarContent>
    </Toolbar>
  );
};

export default DRDashboardToolbar;
