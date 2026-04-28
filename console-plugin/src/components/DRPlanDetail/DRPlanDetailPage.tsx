import { useState, useCallback } from 'react';
import { DocumentTitle } from '@openshift-console/dynamic-plugin-sdk';
import {
  Alert,
  PageSection,
  Skeleton,
  Tab,
  Tabs,
  TabTitleText,
} from '@patternfly/react-core';
import { useParams } from 'react-router-dom';
import DRBreadcrumb from '../shared/DRBreadcrumb';
import PlanHeader from './PlanHeader';
import DRLifecycleDiagram from './DRLifecycleDiagram';
import TransitionProgressBanner from './TransitionProgressBanner';
import { WaveCompositionTree } from './WaveCompositionTree';
import { ExecutionHistoryTable } from './ExecutionHistoryTable';
import { PlanConfiguration } from './PlanConfiguration';
import { PreflightConfirmationModal } from './PreflightConfirmationModal';
import { useDRPlan, useDRExecution, useDRExecutions } from '../../hooks/useDRResources';
import { useCreateDRExecution } from '../../hooks/useCreateDRExecution';
import { getPreflightData } from '../../hooks/usePreflightData';
import { DRPlan } from '../../models/types';
import { getEffectivePhase } from '../../utils/drPlanUtils';
import { WaveProgress } from './DRLifecycleDiagram';

const DRPlanDetailPage: React.FC = () => {
  const { name } = useParams<{ name: string }>();
  const [plan, planLoaded, planError] = useDRPlan(name!);
  const activeExecName = plan?.status?.activeExecution ?? '';
  const [execution] = useDRExecution(activeExecName);
  const [executions, executionsLoaded] = useDRExecutions(name!);
  const [activeTab, setActiveTab] = useState<string | number>(0);
  const [pendingAction, setPendingAction] = useState<string | null>(null);
  const { create, isCreating, error: createError, clearError } = useCreateDRExecution();

  const effectivePhase = plan ? getEffectivePhase(plan) : null;
  const restPhase = plan?.status?.phase;
  const isInTransition = effectivePhase !== null && effectivePhase !== restPhase;

  const waveProgress: WaveProgress | null = (() => {
    const waves = execution?.status?.waves;
    if (!waves || waves.length === 0) return null;
    const completed = waves.filter(w => w.completionTime).length;
    return { current: Math.min(completed + 1, waves.length), total: waves.length };
  })();

  const handleAction = useCallback((_action: string, _plan: DRPlan) => {
    setPendingAction(_action);
  }, []);

  const handleConfirm = useCallback(async () => {
    if (!pendingAction || !plan) return;
    try {
      await create(plan.metadata!.name!, pendingAction);
      setPendingAction(null);
    } catch {
      // Error is stored in the hook state and displayed in the modal
    }
  }, [pendingAction, plan, create]);

  const handleCloseModal = useCallback(() => {
    setPendingAction(null);
    clearError();
  }, [clearError]);

  const preflightData =
    pendingAction && plan && executionsLoaded
      ? getPreflightData(plan, pendingAction, executions)
      : null;

  return (
    <>
      <DocumentTitle>{`DR Plan: ${name}`}</DocumentTitle>
      <PageSection>
        <DRBreadcrumb planName={name} />
      </PageSection>
      <PageSection>
        {planError && (
          <Alert variant="danger" isInline title="Failed to load DR plan">
            {String(planError)}
          </Alert>
        )}
        {!planLoaded && !planError && (
          <>
            <Skeleton width="40%" screenreaderText="Loading plan details" />
            <br />
            <Skeleton width="100%" height="300px" />
          </>
        )}
        {planLoaded && plan && (
          <Tabs activeKey={activeTab} onSelect={(_e, key) => setActiveTab(key)}>
            <Tab eventKey={0} title={<TabTitleText>Overview</TabTitleText>}>
              <PlanHeader plan={plan} />
              {isInTransition && (
                <TransitionProgressBanner plan={plan} execution={execution ?? null} />
              )}
              <DRLifecycleDiagram plan={plan} onAction={handleAction} waveProgress={isInTransition ? waveProgress : null} />
            </Tab>
            <Tab eventKey={1} title={<TabTitleText>Waves</TabTitleText>}>
              <WaveCompositionTree plan={plan} />
            </Tab>
            <Tab eventKey={2} title={<TabTitleText>History</TabTitleText>}>
              {!executionsLoaded ? (
                <Skeleton width="100%" height="200px" screenreaderText="Loading execution history" />
              ) : (
                <ExecutionHistoryTable executions={executions} planName={name!} />
              )}
            </Tab>
            <Tab eventKey={3} title={<TabTitleText>Configuration</TabTitleText>}>
              <PlanConfiguration plan={plan} />
            </Tab>
          </Tabs>
        )}
      </PageSection>
      {pendingAction && plan && preflightData && (
        <PreflightConfirmationModal
          isOpen
          onClose={handleCloseModal}
          onConfirm={handleConfirm}
          action={pendingAction}
          planName={plan.metadata?.name ?? ''}
          preflightData={preflightData}
          isCreating={isCreating}
          error={createError}
        />
      )}
    </>
  );
};

export default DRPlanDetailPage;
