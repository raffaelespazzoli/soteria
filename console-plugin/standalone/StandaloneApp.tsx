import { Switch, Route, Redirect, Link, useLocation } from 'react-router-dom';
import {
  Page,
  Masthead,
  MastheadMain,
  MastheadBrand,
  PageSidebar,
  PageSidebarBody,
  Nav,
  NavList,
  NavItem,
} from '@patternfly/react-core';
import DRDashboardPage from '../src/components/DRDashboard/DRDashboardPage';
import DRPlanDetailPage from '../src/components/DRPlanDetail/DRPlanDetailPage';
import ExecutionDetailPage from '../src/components/ExecutionDetail/ExecutionDetailPage';

function StandaloneApp() {
  const location = useLocation();

  const masthead = (
    <Masthead>
      <MastheadMain>
        <MastheadBrand>Soteria DR Management</MastheadBrand>
      </MastheadMain>
    </Masthead>
  );

  const sidebar = (
    <PageSidebar>
      <PageSidebarBody>
        <Nav aria-label="Main navigation">
          <NavList>
            <NavItem isActive={location.pathname === '/disaster-recovery'}>
              <Link to="/disaster-recovery">Disaster Recovery</Link>
            </NavItem>
          </NavList>
        </Nav>
      </PageSidebarBody>
    </PageSidebar>
  );

  return (
    <Page masthead={masthead} sidebar={sidebar}>
      <Switch>
        <Route exact path="/disaster-recovery" component={DRDashboardPage} />
        <Route exact path="/disaster-recovery/plans/:name" component={DRPlanDetailPage} />
        <Route exact path="/disaster-recovery/executions/:name" component={ExecutionDetailPage} />
        <Redirect to="/disaster-recovery" />
      </Switch>
    </Page>
  );
}

export default StandaloneApp;
