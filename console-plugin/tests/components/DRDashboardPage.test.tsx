import { render, screen, cleanup } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import DRDashboardPage from '../../src/components/DRDashboard/DRDashboardPage';
import {
  saveDashboardState,
  restoreDashboardState,
} from '../../src/hooks/useDashboardState';

expect.extend(toHaveNoViolations);

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  DocumentTitle: ({ children }: { children: React.ReactNode }) => (
    <title>{children}</title>
  ),
}));

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

  it('calls saveDashboardState on unmount', () => {
    render(<DRDashboardPage />);
    expect(mockSave).not.toHaveBeenCalled();
    cleanup();
    expect(mockSave).toHaveBeenCalledTimes(1);
    expect(mockSave).toHaveBeenCalledWith(
      expect.objectContaining({
        scrollTop: expect.any(Number),
        filters: expect.any(Object),
        searchText: expect.any(String),
      }),
    );
  });

  it('calls window.scrollTo when restoreDashboardState returns saved state', () => {
    const scrollToSpy = jest.spyOn(window, 'scrollTo').mockImplementation(() => {});
    mockRestore.mockReturnValue({ scrollTop: 250, filters: {}, searchText: '' });
    render(<DRDashboardPage />);
    expect(scrollToSpy).toHaveBeenCalledWith(0, 250);
    scrollToSpy.mockRestore();
  });

  it('has no accessibility violations', async () => {
    const { container } = render(<DRDashboardPage />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
