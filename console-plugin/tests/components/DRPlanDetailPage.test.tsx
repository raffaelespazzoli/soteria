import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import DRPlanDetailPage from '../../src/components/DRPlanDetail/DRPlanDetailPage';

expect.extend(toHaveNoViolations);

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  DocumentTitle: ({ children }: { children: React.ReactNode }) => (
    <title>{children}</title>
  ),
}));

jest.mock('react-router', () => ({
  ...jest.requireActual('react-router'),
  useParams: () => ({ name: 'erp-full-stack' }),
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

describe('DRPlanDetailPage', () => {
  it('renders the plan name from URL params', () => {
    render(<DRPlanDetailPage />);
    expect(
      screen.getByRole('heading', { name: 'erp-full-stack' }),
    ).toBeInTheDocument();
  });

  it('renders a breadcrumb with Disaster Recovery link', () => {
    render(<DRPlanDetailPage />);
    const drLink = screen.getByRole('link', { name: /disaster recovery/i });
    expect(drLink).toBeInTheDocument();
    expect(drLink).toHaveAttribute('href', '/disaster-recovery');
  });

  it('renders the plan name in both breadcrumb and heading', () => {
    render(<DRPlanDetailPage />);
    const matches = screen.getAllByText('erp-full-stack');
    expect(matches.length).toBeGreaterThanOrEqual(2);
  });

  it('renders a breadcrumb navigation element', () => {
    render(<DRPlanDetailPage />);
    expect(screen.getByRole('navigation', { name: /breadcrumb/i })).toBeInTheDocument();
  });

  it('is the default export', () => {
    expect(DRPlanDetailPage).toBeDefined();
    expect(typeof DRPlanDetailPage).toBe('function');
  });

  it('has no accessibility violations', async () => {
    const { container } = render(<DRPlanDetailPage />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
