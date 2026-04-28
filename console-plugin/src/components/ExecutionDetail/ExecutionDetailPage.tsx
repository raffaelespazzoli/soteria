import { DocumentTitle } from '@openshift-console/dynamic-plugin-sdk';
import { PageSection, Title, Content } from '@patternfly/react-core';
import { useParams } from 'react-router-dom';
import DRBreadcrumb from '../shared/DRBreadcrumb';

const ExecutionDetailPage: React.FC = () => {
  const { name } = useParams<{ name: string }>();

  return (
    <>
      <DocumentTitle>{`DR Execution: ${name}`}</DocumentTitle>
      <PageSection>
        <DRBreadcrumb executionName={name} />
        <Title headingLevel="h1">{name}</Title>
      </PageSection>
      <PageSection>
        <Content component="p">Execution detail content will be implemented in Story 7.2.</Content>
      </PageSection>
    </>
  );
};

export default ExecutionDetailPage;
