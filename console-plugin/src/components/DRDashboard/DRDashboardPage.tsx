import { useCallback, useEffect } from 'react';
import { DocumentTitle } from '@openshift-console/dynamic-plugin-sdk';
import { PageSection, Title } from '@patternfly/react-core';
import DRDashboard from './DRDashboard';
import AlertBannerSystem from './AlertBannerSystem';
import { restoreDashboardState } from '../../hooks/useDashboardState';
import { useDRPlans } from '../../hooks/useDRResources';
import { useFilterParams, EMPTY_FILTERS } from '../../hooks/useFilterParams';

function DRDashboardPage() {
  const [plans] = useDRPlans();
  const { setFilters } = useFilterParams();

  const handleFilterByHealth = useCallback(
    (healthStatus: string) => {
      setFilters({ ...EMPTY_FILTERS, protected: [healthStatus] });
    },
    [setFilters],
  );

  useEffect(() => {
    const saved = restoreDashboardState();
    if (saved) {
      window.scrollTo(0, saved.scrollTop);
    }
  }, []);

  return (
    <>
      <DocumentTitle>Disaster Recovery</DocumentTitle>
      <PageSection>
        <Title headingLevel="h1">Disaster Recovery</Title>
      </PageSection>
      <PageSection>
        <AlertBannerSystem plans={plans ?? []} onFilterByHealth={handleFilterByHealth} />
        <DRDashboard />
      </PageSection>
    </>
  );
}

export default DRDashboardPage;
