import {
  CodeBlock,
  CodeBlockCode,
  Content,
  ContentVariants,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  Label,
  LabelGroup,
} from '@patternfly/react-core';
import { DRPlan } from '../../models/types';
import { ReplicationHealthExpanded } from './ReplicationHealthExpanded';

const INTERNAL_ANNOTATION_PREFIXES = [
  'kubernetes.io/',
  'kubectl.kubernetes.io/',
  'control-plane.alpha.kubernetes.io/',
];

function isInternalAnnotation(key: string): boolean {
  return INTERNAL_ANNOTATION_PREFIXES.some((prefix) => key.includes(prefix));
}

function formatCreationDate(timestamp: string | undefined): string {
  if (!timestamp) return 'N/A';
  return new Date(timestamp).toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

interface PlanConfigurationProps {
  plan: DRPlan;
}

export const PlanConfiguration: React.FC<PlanConfigurationProps> = ({ plan }) => {
  const labels = plan.metadata?.labels ?? {};
  const annotations = plan.metadata?.annotations ?? {};
  const externalAnnotations = Object.entries(annotations).filter(
    ([key]) => !isInternalAnnotation(key),
  );

  const specText = JSON.stringify(plan.spec, null, 2);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--pf-t--global--spacer--lg)' }}>
      <DescriptionList isHorizontal isCompact>
        <DescriptionListGroup>
          <DescriptionListTerm>Name</DescriptionListTerm>
          <DescriptionListDescription>{plan.metadata?.name}</DescriptionListDescription>
        </DescriptionListGroup>
        <DescriptionListGroup>
          <DescriptionListTerm>Label Selector</DescriptionListTerm>
          <DescriptionListDescription>
            {plan.spec?.labelSelector ? <code>{plan.spec.labelSelector}</code> : <i>None</i>}
          </DescriptionListDescription>
        </DescriptionListGroup>
        <DescriptionListGroup>
          <DescriptionListTerm>Wave Label</DescriptionListTerm>
          <DescriptionListDescription>{plan.spec?.waveLabel}</DescriptionListDescription>
        </DescriptionListGroup>
        <DescriptionListGroup>
          <DescriptionListTerm>Max Concurrent Failovers</DescriptionListTerm>
          <DescriptionListDescription>{plan.spec?.maxConcurrentFailovers}</DescriptionListDescription>
        </DescriptionListGroup>
        <DescriptionListGroup>
          <DescriptionListTerm>Primary Site</DescriptionListTerm>
          <DescriptionListDescription>{plan.spec?.primarySite}</DescriptionListDescription>
        </DescriptionListGroup>
        <DescriptionListGroup>
          <DescriptionListTerm>Secondary Site</DescriptionListTerm>
          <DescriptionListDescription>{plan.spec?.secondarySite}</DescriptionListDescription>
        </DescriptionListGroup>
        <DescriptionListGroup>
          <DescriptionListTerm>Created</DescriptionListTerm>
          <DescriptionListDescription>
            {formatCreationDate(plan.metadata?.creationTimestamp)}
          </DescriptionListDescription>
        </DescriptionListGroup>
      </DescriptionList>

      {Object.keys(labels).length > 0 && (
        <div>
          <Content component={ContentVariants.h3}>Labels</Content>
          <LabelGroup>
            {Object.entries(labels).map(([key, value]) => (
              <Label key={key} isCompact>
                {key}={value}
              </Label>
            ))}
          </LabelGroup>
        </div>
      )}

      {externalAnnotations.length > 0 && (
        <div>
          <Content component={ContentVariants.h3}>Annotations</Content>
          <DescriptionList isHorizontal isCompact>
            {externalAnnotations.map(([key, value]) => (
              <DescriptionListGroup key={key}>
                <DescriptionListTerm>{key}</DescriptionListTerm>
                <DescriptionListDescription>{value}</DescriptionListDescription>
              </DescriptionListGroup>
            ))}
          </DescriptionList>
        </div>
      )}

      <div>
        <Content component={ContentVariants.h3}>Replication Health</Content>
        <ReplicationHealthExpanded plan={plan} />
      </div>

      <div>
        <Content component={ContentVariants.h3}>Plan Spec</Content>
        <CodeBlock>
          <CodeBlockCode id="plan-spec-yaml">{specText}</CodeBlockCode>
        </CodeBlock>
      </div>
    </div>
  );
};
