import { useState } from 'react';
import { DocumentTitle } from '@openshift-console/dynamic-plugin-sdk';
import {
  Alert,
  PageSection,
  Skeleton,
  Tab,
  Tabs,
  TabTitleText,
} from '@patternfly/react-core';
import { useParams } from 'react-router';
import DRBreadcrumb from '../shared/DRBreadcrumb';
import PlanHeader from './PlanHeader';
import DRLifecycleDiagram from './DRLifecycleDiagram';
import TransitionProgressBanner from './TransitionProgressBanner';
import { WaveCompositionTree } from './WaveCompositionTree';
import { ExecutionHistoryTable } from './ExecutionHistoryTable';
import { PlanConfiguration } from './PlanConfiguration';
import { useDRPlan, useDRExecution, useDRExecutions } from '../../hooks/useDRResources';
import { DRPlan } from '../../models/types';
import { getEffectivePhase } from '../../utils/drPlanUtils';
import { WaveProgress } from './DRLifecycleDiagram';

function handleAction(action: string, plan: DRPlan) {
  console.log('Trigger:', action, plan.metadata?.name);
}

const DRPlanDetailPage: React.FC = () => {
  const { name } = useParams<{ name: string }>();
  const [plan, planLoaded, planError] = useDRPlan(name!);
  const activeExecName = plan?.status?.activeExecution ?? '';
  const [execution] = useDRExecution(activeExecName);
  const [executions, executionsLoaded] = useDRExecutions(name!);
  const [activeTab, setActiveTab] = useState<string | number>(0);

  const effectivePhase = plan ? getEffectivePhase(plan) : null;
  const restPhase = plan?.status?.phase;
  const isInTransition = effectivePhase !== null && effectivePhase !== restPhase;

  const waveProgress: WaveProgress | null = (() => {
    const waves = execution?.status?.waves;
    if (!waves || waves.length === 0) return null;
    const completed = waves.filter(w => w.completionTime).length;
    return { current: Math.min(completed + 1, waves.length), total: waves.length };
  })();

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
    </>
  );
};

export default DRPlanDetailPage;
