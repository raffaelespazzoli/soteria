import { Breadcrumb, BreadcrumbItem } from '@patternfly/react-core';
import { Link } from 'react-router-dom';

interface DRBreadcrumbProps {
  planName?: string;
  executionName?: string;
}

const DRBreadcrumb: React.FC<DRBreadcrumbProps> = ({ planName, executionName }) => (
  <Breadcrumb>
    <BreadcrumbItem>
      <Link to="/disaster-recovery">Disaster Recovery</Link>
    </BreadcrumbItem>
    {planName && (
      <BreadcrumbItem isActive={!executionName}>
        {executionName ? (
          <Link to={`/disaster-recovery/plans/${planName}`}>{planName}</Link>
        ) : (
          planName
        )}
      </BreadcrumbItem>
    )}
    {executionName && <BreadcrumbItem isActive>{executionName}</BreadcrumbItem>}
  </Breadcrumb>
);

export default DRBreadcrumb;
