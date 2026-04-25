import { DocumentTitle } from '@openshift-console/dynamic-plugin-sdk';
import { PageSection, Title, Content } from '@patternfly/react-core';
import { useParams } from 'react-router';
import DRBreadcrumb from '../shared/DRBreadcrumb';

const DRPlanDetailPage: React.FC = () => {
  const { name } = useParams<{ name: string }>();

  return (
    <>
      <DocumentTitle>{`DR Plan: ${name}`}</DocumentTitle>
      <PageSection>
        <DRBreadcrumb planName={name} />
        <Title headingLevel="h1">{name}</Title>
      </PageSection>
      <PageSection>
        <Content component="p">
          Plan detail content will be implemented in Story 6.5.
        </Content>
      </PageSection>
    </>
  );
};

export default DRPlanDetailPage;
