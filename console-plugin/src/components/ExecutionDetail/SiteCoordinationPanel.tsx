import {
  Card,
  CardBody,
  CardTitle,
  ProgressStep,
  ProgressStepper,
  ProgressStepProps,
  Spinner,
  Split,
  SplitItem,
} from '@patternfly/react-core';
import { CheckCircleIcon, PendingIcon } from '@patternfly/react-icons';
import { DRExecution, ExecutionMode, SiteCoordinationStatus } from '../../models/types';

interface SiteStep {
  label: string;
  done: boolean;
}

function getSourceSteps(sourceStatus: SiteCoordinationStatus | undefined): SiteStep[] {
  return [
    { label: 'Demoting Volumes', done: !!sourceStatus?.demotionComplete },
    { label: 'Demotion Synced', done: !!sourceStatus?.demotionComplete },
  ];
}

function getTargetSteps(site: SiteCoordinationStatus | undefined): SiteStep[] {
  return [
    { label: 'Promoting Volumes', done: !!site?.step0Complete },
  ];
}

function stepVariant(step: SiteStep, isActive: boolean): NonNullable<ProgressStepProps['variant']> {
  if (step.done) return 'success';
  if (isActive) return 'info';
  return 'pending';
}

function isStepCurrent(step: SiteStep, isActive: boolean): boolean {
  return !step.done && isActive;
}

function relativeTime(iso: string | undefined): string {
  if (!iso) return '';
  const delta = Date.now() - new Date(iso).getTime();
  if (delta < 1000) return 'just now';
  const secs = Math.floor(delta / 1000);
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.floor(secs / 60);
  return `${mins}m ago`;
}

interface SiteLaneProps {
  label: string;
  siteName: string;
  steps: SiteStep[];
  lastUpdated?: string;
}

const SiteLane: React.FC<SiteLaneProps> = ({ label, siteName, steps, lastUpdated }) => {
  const allDone = steps.every((s) => s.done);
  const firstPending = steps.findIndex((s) => !s.done);
  const subtitle = lastUpdated ? `Updated ${relativeTime(lastUpdated)}` : undefined;

  return (
    <div data-testid={`site-lane-${siteName}`}>
      <div
        style={{
          fontWeight: 'var(--pf-t--global--font--weight--heading--default, 700)' as React.CSSProperties['fontWeight'],
          marginBottom: 'var(--pf-t--global--spacer--xs, var(--pf-v5-global--spacer--xs))',
          fontSize: 'var(--pf-t--global--font--size--body--lg, var(--pf-v5-global--FontSize--lg))',
        }}
      >
        {label}{' '}
        <span
          style={{
            fontWeight: 'normal' as React.CSSProperties['fontWeight'],
            color: 'var(--pf-t--global--text--color--subtle, var(--pf-v5-global--Color--200))',
          }}
        >
          ({siteName})
        </span>
      </div>
      <ProgressStepper aria-label={`${label} steps`}>
        {steps.map((step, idx) => {
          const isActive = idx === firstPending;
          return (
            <ProgressStep
              key={step.label}
              variant={stepVariant(step, isActive)}
              isCurrent={isStepCurrent(step, isActive)}
              id={`${siteName}-step-${idx}`}
              titleId={`${siteName}-step-${idx}-title`}
              icon={
                step.done ? (
                  <CheckCircleIcon />
                ) : isActive ? (
                  <Spinner size="md" aria-label={`${step.label} in progress`} />
                ) : (
                  <PendingIcon />
                )
              }
              aria-label={`${step.label}: ${step.done ? 'complete' : isActive ? 'in progress' : 'pending'}`}
            >
              {step.label}
            </ProgressStep>
          );
        })}
      </ProgressStepper>
      {subtitle && !allDone && (
        <div
          style={{
            color: 'var(--pf-t--global--text--color--subtle, var(--pf-v5-global--Color--200))',
            fontSize: 'var(--pf-t--global--font--size--body--sm, var(--pf-v5-global--FontSize--sm))',
            marginTop: 'var(--pf-t--global--spacer--xs, var(--pf-v5-global--spacer--xs))',
          }}
        >
          {subtitle}
        </div>
      )}
    </div>
  );
};

interface SiteCoordinationPanelProps {
  execution: DRExecution;
  sourceSite: string;
  targetSite: string;
}

const SiteCoordinationPanel: React.FC<SiteCoordinationPanelProps> = ({
  execution,
  sourceSite,
  targetSite,
}) => {
  const mode = execution.spec.mode;
  const siteStatuses = execution.status?.siteStatuses;

  if (mode === ExecutionMode.Disaster) return null;
  if (!siteStatuses && !execution.status?.isActive) return null;

  const sourceStatus = siteStatuses?.[sourceSite];
  const targetStatus = siteStatuses?.[targetSite];

  const isPlanned = mode === ExecutionMode.PlannedMigration;
  const sourceSteps = isPlanned ? getSourceSteps(sourceStatus) : [];
  const targetSteps = isPlanned
    ? getTargetSteps(targetStatus)
    : [];

  if (sourceSteps.length === 0 && targetSteps.length === 0) return null;

  const allComplete = [...sourceSteps, ...targetSteps].every((s) => s.done);
  const wavesStarted = (execution.status?.waves?.length ?? 0) > 0;

  if (allComplete && wavesStarted) return null;

  return (
    <Card
      isCompact
      data-testid="site-coordination-panel"
      style={{
        marginBottom: 'var(--pf-t--global--spacer--md, var(--pf-v5-global--spacer--md))',
      }}
    >
      <CardTitle>Site Coordination</CardTitle>
      <CardBody>
        <Split hasGutter>
          {sourceSteps.length > 0 && (
            <SplitItem isFilled>
              <SiteLane
                label="Source"
                siteName={sourceSite}
                steps={sourceSteps}
                lastUpdated={sourceStatus?.lastUpdated}
              />
            </SplitItem>
          )}
          {targetSteps.length > 0 && (
            <SplitItem isFilled>
              <SiteLane
                label={isPlanned ? 'Target' : 'Passive'}
                siteName={targetSite}
                steps={targetSteps}
                lastUpdated={targetStatus?.lastUpdated}
              />
            </SplitItem>
          )}
        </Split>
      </CardBody>
    </Card>
  );
};

export default SiteCoordinationPanel;
