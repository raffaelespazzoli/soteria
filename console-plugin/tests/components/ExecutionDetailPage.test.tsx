import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import ExecutionDetailPage from '../../src/components/ExecutionDetail/ExecutionDetailPage';

expect.extend(toHaveNoViolations);

jest.mock('@openshift-console/dynamic-plugin-sdk', () => ({
  DocumentTitle: ({ children }: { children: React.ReactNode }) => (
    <title>{children}</title>
  ),
}));

jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useParams: () => ({ name: 'exec-20260425-001' }),
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

describe('ExecutionDetailPage', () => {
  it('renders the execution name from URL params', () => {
    render(<ExecutionDetailPage />);
    expect(
      screen.getByRole('heading', { name: 'exec-20260425-001' }),
    ).toBeInTheDocument();
  });

  it('renders a breadcrumb with Disaster Recovery link', () => {
    render(<ExecutionDetailPage />);
    const drLink = screen.getByRole('link', { name: /disaster recovery/i });
    expect(drLink).toBeInTheDocument();
    expect(drLink).toHaveAttribute('href', '/disaster-recovery');
  });

  it('renders the execution name in both breadcrumb and heading', () => {
    render(<ExecutionDetailPage />);
    const matches = screen.getAllByText('exec-20260425-001');
    expect(matches.length).toBeGreaterThanOrEqual(2);
  });

  it('renders a breadcrumb navigation element', () => {
    render(<ExecutionDetailPage />);
    expect(screen.getByRole('navigation', { name: /breadcrumb/i })).toBeInTheDocument();
  });

  it('is the default export', () => {
    expect(ExecutionDetailPage).toBeDefined();
    expect(typeof ExecutionDetailPage).toBe('function');
  });

  it('has no accessibility violations', async () => {
    const { container } = render(<ExecutionDetailPage />);
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
