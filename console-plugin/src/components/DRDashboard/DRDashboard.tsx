import { DocumentTitle, ListPageHeader } from '@openshift-console/dynamic-plugin-sdk';
import { PageSection, Content } from '@patternfly/react-core';

export default function DRDashboard() {
  return (
    <>
      <DocumentTitle>Disaster Recovery</DocumentTitle>
      <ListPageHeader title="Disaster Recovery" />
      <PageSection>
        <Content component="p">DR Dashboard will be implemented in Story 6.3.</Content>
      </PageSection>
    </>
  );
}
