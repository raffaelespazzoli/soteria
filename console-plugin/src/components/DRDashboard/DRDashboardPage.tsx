import { useEffect } from 'react';
import { DocumentTitle } from '@openshift-console/dynamic-plugin-sdk';
import { PageSection, Title } from '@patternfly/react-core';
import DRDashboard from './DRDashboard';
import {
  restoreDashboardState,
  saveDashboardState,
} from '../../hooks/useDashboardState';

const DRDashboardPage: React.FC = () => {
  useEffect(() => {
    const saved = restoreDashboardState();
    if (saved) {
      window.scrollTo(0, saved.scrollTop);
    }

    return () => {
      saveDashboardState({
        scrollTop: window.scrollY,
        filters: {},
        searchText: '',
      });
    };
  }, []);

  return (
    <>
      <DocumentTitle>Disaster Recovery</DocumentTitle>
      <PageSection>
        <Title headingLevel="h1">Disaster Recovery</Title>
      </PageSection>
      <PageSection>
        <DRDashboard />
      </PageSection>
    </>
  );
};

export default DRDashboardPage;
