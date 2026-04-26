import { render, screen } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';
import { DashboardEmptyState } from '../../src/components/DRDashboard/DashboardEmptyState';

expect.extend(toHaveNoViolations);

describe('DashboardEmptyState', () => {
  it('renders empty state with title and guidance', () => {
    render(<DashboardEmptyState />);
    expect(screen.getByText('No DR Plans configured')).toBeInTheDocument();
    expect(screen.getByText(/Create your first DR plan/)).toBeInTheDocument();
    expect(screen.getByText('View documentation')).toBeInTheDocument();
  });

  it('renders label guidance text with code blocks', () => {
    render(<DashboardEmptyState />);
    expect(screen.getByText(/app\.kubernetes\.io\/part-of/)).toBeInTheDocument();
    expect(screen.getByText(/soteria\.io\/wave/)).toBeInTheDocument();
  });

  it('documentation link opens in new tab', () => {
    render(<DashboardEmptyState />);
    const link = screen.getByRole('link', { name: /view documentation/i });
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'));
  });

  it('documentation link points to getting-started docs', () => {
    render(<DashboardEmptyState />);
    const link = screen.getByRole('link', { name: /view documentation/i });
    expect(link).toHaveAttribute('href', expect.stringContaining('getting-started'));
  });

  it('renders heading at h4 level', () => {
    render(<DashboardEmptyState />);
    const heading = screen.getByRole('heading', { level: 4, name: 'No DR Plans configured' });
    expect(heading).toBeInTheDocument();
  });

  it('passes jest-axe', async () => {
    const { container } = render(<DashboardEmptyState />);
    expect(await axe(container)).toHaveNoViolations();
  });
});
