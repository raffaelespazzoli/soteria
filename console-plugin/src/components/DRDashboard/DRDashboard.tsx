import { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert } from '@patternfly/react-core';
import { Table, Thead, Tr, Th, Tbody, Td, ThProps } from '@patternfly/react-table';
import { Link } from 'react-router';
import { useDRPlans, useDRExecutions } from '../../hooks/useDRResources';
import { useFilterParams, FilterState } from '../../hooks/useFilterParams';
import { saveDashboardState, restoreDashboardState } from '../../hooks/useDashboardState';
import {
  getEffectivePhase,
  getReplicationHealth,
  buildLatestExecutionMap,
  HEALTH_SORT_ORDER,
  EffectivePhase,
  ReplicationHealth,
} from '../../utils/drPlanUtils';
import { formatRelativeTime } from '../../utils/formatters';
import PhaseBadge from '../shared/PhaseBadge';
import ExecutionResultBadge from '../shared/ExecutionResultBadge';
import ReplicationHealthIndicator from '../shared/ReplicationHealthIndicator';
import DRDashboardToolbar from './DRDashboardToolbar';
import DRPlanActions from './DRPlanActions';
import { DashboardEmptyState } from './DashboardEmptyState';
import { DRExecution, DRExecutionResult, DRPlan } from '../../models/types';

type SortColumn = 0 | 1 | 2 | 3 | 4;

const COLUMN_NAMES = ['Name', 'Phase', 'Active On', 'Protected', 'Last Execution'];

function useDebounce<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}

interface EnrichedPlan {
  plan: DRPlan;
  effectivePhase: EffectivePhase;
  health: ReplicationHealth;
  lastExec: DRExecution | null;
}

function enrichPlans(plans: DRPlan[], executions: DRExecution[]): EnrichedPlan[] {
  const latestByPlan = buildLatestExecutionMap(executions);
  return plans.map((plan) => ({
    plan,
    effectivePhase: getEffectivePhase(plan),
    health: getReplicationHealth(plan),
    lastExec: latestByPlan.get(plan.metadata?.name ?? '') ?? null,
  }));
}

function getLastExecResult(ep: EnrichedPlan): string {
  if (!ep.lastExec) return 'Never';
  return ep.lastExec.status?.result ?? 'InProgress';
}

function filterPlans(
  enriched: EnrichedPlan[],
  filters: FilterState,
  debouncedSearch: string,
): EnrichedPlan[] {
  return enriched.filter((ep) => {
    const name = ep.plan.metadata?.name ?? '';
    if (debouncedSearch && !name.toLowerCase().includes(debouncedSearch.toLowerCase())) {
      return false;
    }
    if (filters.phase.length > 0 && !filters.phase.includes(ep.effectivePhase)) {
      return false;
    }
    if (
      filters.activeOn.length > 0 &&
      !filters.activeOn.includes(ep.plan.status?.activeSite ?? '')
    ) {
      return false;
    }
    if (filters.protected.length > 0 && !filters.protected.includes(ep.health.status)) {
      return false;
    }
    if (filters.lastExecution.length > 0) {
      const result = getLastExecResult(ep);
      if (!filters.lastExecution.includes(result)) return false;
    }
    return true;
  });
}

function sortPlans(
  plans: EnrichedPlan[],
  sortIndex: SortColumn,
  sortDirection: 'asc' | 'desc',
): EnrichedPlan[] {
  const sorted = [...plans];
  sorted.sort((a, b) => {
    let cmp = 0;
    switch (sortIndex) {
      case 0: // Name
        cmp = (a.plan.metadata?.name ?? '').localeCompare(b.plan.metadata?.name ?? '');
        break;
      case 1: // Phase
        cmp = a.effectivePhase.localeCompare(b.effectivePhase);
        break;
      case 2: // Active On
        cmp = (a.plan.status?.activeSite ?? '').localeCompare(b.plan.status?.activeSite ?? '');
        break;
      case 3: // Protected
        cmp =
          (HEALTH_SORT_ORDER[a.health.status] ?? 99) - (HEALTH_SORT_ORDER[b.health.status] ?? 99);
        break;
      case 4: {
        // Last Execution
        const timeA = a.lastExec?.status?.startTime
          ? new Date(a.lastExec.status.startTime).getTime()
          : 0;
        const timeB = b.lastExec?.status?.startTime
          ? new Date(b.lastExec.status.startTime).getTime()
          : 0;
        cmp = timeB - timeA;
        break;
      }
    }
    return sortDirection === 'asc' ? cmp : -cmp;
  });
  return sorted;
}

export default function DRDashboard() {
  const [plans, plansLoaded, plansError] = useDRPlans();
  const [executions, execsLoaded] = useDRExecutions();
  const { filters, setFilters, clearAllFilters } = useFilterParams();

  const [sortIndex, setSortIndex] = useState<SortColumn>(3);
  const [sortDirection, setSortDirection] = useState<'asc' | 'desc'>('asc');

  const debouncedSearch = useDebounce(filters.search, 300);

  // Restore saved state on mount — but only when the URL has no filter params.
  // URL params win so that shareable/bookmarked links work (AC4).
  useEffect(() => {
    const hasUrlFilters =
      filters.search ||
      filters.phase.length > 0 ||
      filters.activeOn.length > 0 ||
      filters.protected.length > 0 ||
      filters.lastExecution.length > 0;
    if (hasUrlFilters) return;

    const saved = restoreDashboardState();
    if (saved) {
      if (saved.searchText || Object.values(saved.filters).some((f) => f.length > 0)) {
        setFilters({
          search: saved.searchText,
          phase: saved.filters.phase ?? [],
          activeOn: saved.filters.activeOn ?? [],
          protected: saved.filters.protected ?? [],
          lastExecution: saved.filters.lastExecution ?? [],
        });
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Save state on unmount
  useEffect(() => {
    return () => {
      saveDashboardState({
        scrollTop: window.scrollY,
        filters: {
          phase: filters.phase,
          activeOn: filters.activeOn,
          protected: filters.protected,
          lastExecution: filters.lastExecution,
        },
        searchText: filters.search,
      });
    };
  }, [filters]);

  const enriched = useMemo(
    () => (plansLoaded && execsLoaded ? enrichPlans(plans, executions) : []),
    [plans, executions, plansLoaded, execsLoaded],
  );

  const filtered = useMemo(
    () => filterPlans(enriched, filters, debouncedSearch),
    [enriched, filters, debouncedSearch],
  );

  const sorted = useMemo(
    () => sortPlans(filtered, sortIndex, sortDirection),
    [filtered, sortIndex, sortDirection],
  );

  const getSortParams = useCallback(
    (columnIndex: SortColumn): ThProps['sort'] => ({
      sortBy: {
        index: sortIndex,
        direction: sortDirection,
      },
      onSort: (_event, index, direction) => {
        setSortIndex(index as SortColumn);
        setSortDirection(direction);
      },
      columnIndex,
    }),
    [sortIndex, sortDirection],
  );

  const onFiltersChange = useCallback(
    (newFilters: FilterState) => {
      setFilters(newFilters);
    },
    [setFilters],
  );

  if (!plansLoaded || !execsLoaded) {
    return <div>Loading...</div>;
  }

  if (plansError) {
    return <Alert variant="danger" isInline title="Failed to load DR plans">{String(plansError)}</Alert>;
  }

  if (plans.length === 0) {
    return <DashboardEmptyState />;
  }

  return (
    <>
      <DRDashboardToolbar
        filters={filters}
        onFiltersChange={onFiltersChange}
        onClearAll={clearAllFilters}
        plans={plans}
        filteredCount={sorted.length}
        totalCount={enriched.length}
      />
      <Table aria-label="DR Plans" variant="compact">
        <Thead>
          <Tr>
            <Th sort={getSortParams(0)}>{COLUMN_NAMES[0]}</Th>
            <Th sort={getSortParams(1)}>{COLUMN_NAMES[1]}</Th>
            <Th sort={getSortParams(2)}>{COLUMN_NAMES[2]}</Th>
            <Th sort={getSortParams(3)}>{COLUMN_NAMES[3]}</Th>
            <Th sort={getSortParams(4)}>{COLUMN_NAMES[4]}</Th>
            <Th>Actions</Th>
          </Tr>
        </Thead>
        <Tbody>
          {sorted.map((ep) => {
            const name = ep.plan.metadata?.name ?? '';
            return (
              <Tr key={name}>
                <Td dataLabel={COLUMN_NAMES[0]}>
                  <Link to={`/disaster-recovery/plans/${name}`}>{name}</Link>
                </Td>
                <Td dataLabel={COLUMN_NAMES[1]}>
                  <PhaseBadge phase={ep.effectivePhase} />
                </Td>
                <Td dataLabel={COLUMN_NAMES[2]}>{ep.plan.status?.activeSite ?? ''}</Td>
                <Td dataLabel={COLUMN_NAMES[3]}>
                  <ReplicationHealthIndicator health={ep.health} />
                </Td>
                <Td dataLabel={COLUMN_NAMES[4]}>
                  <LastExecutionCell enrichedPlan={ep} />
                </Td>
                <Td isActionCell>
                  <DRPlanActions plan={ep.plan} />
                </Td>
              </Tr>
            );
          })}
        </Tbody>
      </Table>
    </>
  );
}

function LastExecutionCell({ enrichedPlan }: { enrichedPlan: EnrichedPlan }) {
  const { lastExec } = enrichedPlan;
  if (!lastExec) return <span>Never</span>;

  const result = lastExec.status?.result as DRExecutionResult | undefined;
  const time = lastExec.status?.completionTime ?? lastExec.status?.startTime;
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.5rem' }}>
      {time && <span>{formatRelativeTime(time)}</span>}
      {result && <ExecutionResultBadge result={result} />}
    </span>
  );
}
