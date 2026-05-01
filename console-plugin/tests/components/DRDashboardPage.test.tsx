import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import { useK8sWatchResource } from '@openshift-console/dynamic-plugin-sdk';
import DRDashboardPage from '../../src/components/DRDashboard/DRDashboardPage';
import {
  saveDashboardState,
  restoreDashboardState,
} from '../../src/hooks/useDashboardState';
import { DRPlan } from '../../src/models/types';

expect.extend(toHaveNoViolations);

const mockReplace = jest.fn();
const mockPush = jest.fn();

jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useLocation: () => ({ search: '', pathname: '/disaster-recovery' }),
  useHistory: () => ({ replace: mockReplace, push: mockPush, location: { search: '' } }),
}));

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  DocumentTitle: ({ children }: { children: React.ReactNode }) => (
    <title>{children}</title>
  ),
  useK8sWatchResource: jest.fn(() => [[], true, null]),
}));

const mockUseK8sWatchResource = useK8sWatchResource as jest.Mock;

jest.mock('../../src/components/DRDashboard/DRDashboard', () => {
  return {
    __esModule: true,
    default: () => <div data-testid="dr-dashboard">Dashboard content</div>,
  };
});

jest.mock('../../src/hooks/useDashboardState', () => ({
  saveDashboardState: jest.fn(),
  restoreDashboardState: jest.fn(() => null),
}));

const mockSave = saveDashboardState as jest.Mock;
const mockRestore = restoreDashboardState as jest.Mock;

beforeEach(() => {
  mockSave.mockClear();
  mockRestore.mockClear().mockReturnValue(null);
  mockReplace.mockClear();
  mockPush.mockClear();
});

describe('DRDashboardPage', () => {
  it('renders the page heading', () => {
    render(<DRDashboardPage />);
    expect(
      screen.getByRole('heading', { name: /disaster recovery/i }),
    ).toBeInTheDocument();
  });

  it('renders the DRDashboard child component', () => {
    render(<DRDashboardPage />);
    expect(screen.getByTestId('dr-dashboard')).toBeInTheDocument();
  });

  it('is the default export', () => {
    expect(DRDashboardPage).toBeDefined();
    expect(typeof DRDashboardPage).toBe('function');
  });

  it('calls restoreDashboardState on mount', () => {
    render(<DRDashboardPage />);
    expect(mockRestore).toHaveBeenCalledTimes(1);
  });

  it('does not call saveDashboardState directly (delegated to DRDashboard)', () => {
    render(<DRDashboardPage />);
    cleanup();
    expect(mockSave).not.toHaveBeenCalled();
  });

  it('calls window.scrollTo when restoreDashboardState returns saved state', () => {
    const scrollToSpy = jest.spyOn(window, 'scrollTo').mockImplementation(() => {});
    mockRestore.mockReturnValue({ scrollTop: 250, filters: {}, searchText: '' });
    render(<DRDashboardPage />);
    expect(scrollToSpy).toHaveBeenCalledWith(0, 250);
    scrollToSpy.mockRestore();
  });

  it('renders ToastContainer for execution notifications', () => {
    render(<DRDashboardPage />);
    const alertGroup = document.querySelector('[class*="pf-v6-c-alert-group"]');
    expect(alertGroup).toBeInTheDocument();
  });

  it('has no accessibility violations', async () => {
    const { container } = render(<DRDashboardPage />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('banner action link navigates with only protected filter, clearing other filters', () => {
    const errorPlan: DRPlan = {
      apiVersion: 'soteria.io/v1alpha1',
      kind: 'DRPlan',
      metadata: { name: 'broken-plan', uid: 'uid-1', creationTimestamp: '' },
      spec: {
        maxConcurrentFailovers: 1,
        primarySite: 'site-a',
        secondarySite: 'site-b',
      },
      status: {
        phase: 'SteadyState',
        conditions: [
          { type: 'ReplicationHealthy', status: 'False', reason: 'Error', message: 'broken' },
        ],
      },
    };
    mockUseK8sWatchResource.mockReturnValue([[errorPlan], true, null]);

    render(<DRDashboardPage />);
    const link = screen.getByText('View affected plans');
    fireEvent.click(link);

    expect(mockReplace).toHaveBeenCalledWith(
      { search: 'protected=Error' },
    );

    mockUseK8sWatchResource.mockReturnValue([[], true, null]);
  });
});
